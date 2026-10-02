"""Narrow descriptor SQL boundary tests; successful admitted read is separate."""
from uuid import uuid4
from test_v26_region_source import _region_scope_fixture


def test_region_descriptor_read_scope_and_role_boundary(db):
    tenant, actor, session, plan, *_ = _region_scope_fixture(db)
    asset = plan['asset_id']
    with db.transaction():
        db.execute("SELECT set_config('app.tenant_id',%s,true)", (str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        try:
            for args in [(tenant, uuid4(), asset, actor, session),
                         (uuid4(), uuid4(), asset, actor, session),
                         (tenant, uuid4(), asset, uuid4(), session)]:
                assert db.execute('SELECT * FROM photo.v26_region_descriptor_node(%s,%s,%s,%s,%s)', args).fetchall() == []
        finally:
            db.execute('RESET ROLE')
    signature = 'photo.v26_region_descriptor_node(uuid,uuid,uuid,uuid,uuid)'
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')", (signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_worker',%s,'EXECUTE')", (signature,)).fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_scheduler',%s,'EXECUTE')", (signature,)).fetchone()[0]


def test_populated_descriptor_read_requires_live_access_and_complete_closure(db):
    from hashlib import sha256
    import json
    from psycopg.types.json import Jsonb
    tenant, actor, session, plan, *_ = _region_scope_fixture(db)
    project, asset = plan['project_id'], plan['asset_id']
    plan['frozen_recipe'].update(icc_lock_sha='a'*64, model_lock_sha='b'*64)
    body = json.dumps(plan, sort_keys=True, separators=(',', ':'))
    digest = sha256(body.encode()).hexdigest()
    def artifact(key, digest):
        return db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,%s,'application/json','document',now()) RETURNING id", (tenant,key,digest,len(body))).fetchone()[0]
    params = artifact('descriptor-params', digest)
    manifest, chunk = artifact('descriptor-manifest','e'*64), artifact('descriptor-chunk','f'*64)
    task, snapshot = uuid4(), db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count) VALUES(%s,'v2','user',%s,%s,1) RETURNING id", (tenant,actor,'a'*64)).fetchone()[0]
    profile = dict(renderer_version='v2.2',backend='cuda',icc_lock_sha='a'*64,model_lock_sha='b'*64,region_runtime_sha256='c'*64)
    node = dict(task_id=str(task),params=dict(artifact_id=str(params),sha256=digest),params_jcs=body,input_digest='d'*64,profile=profile)
    config = dict(project_id=str(project),v26_region=dict(auth_session_id=str(session),nodes=[node]))
    run = db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,'v2','running',%s,%s,%s,%s,'fail_fast',true) RETURNING id", (tenant,snapshot,Jsonb(config),sha256(json.dumps(config,sort_keys=True,separators=(',',':')).encode()).hexdigest(),actor)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",(tenant,project,run,snapshot,'e'*64))
    manifest_ref = dict(artifact_id=str(manifest),sha256='e'*64)
    execution = dict(profile, contracts_sha='4004dd5e69d67835b7d295bfcebded5c006125256c2f6047fd11b2f2dca0f1b4',v2_input_manifest=manifest_ref,v2_entry_count=1,v2_entries_sha='a'*64)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'region.preview:0','region.preview','1','gpu',%s,%s,%s,%s)",(task,tenant,run,Jsonb([manifest_ref]),params,'d'*64,Jsonb(execution)))
    for ordinal,(artifact_id,sha,contract) in enumerate([(params,digest,'Params'),(manifest,'e'*64,'InputClosureManifest'),(chunk,'f'*64,'InputClosureChunk')]):
        db.execute('INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,contract,ordinal) VALUES(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,task,artifact_id,sha,contract,ordinal))
    def read(principal=actor,auth_session=session,selected_asset=asset):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_api')
            try:
                return db.execute('SELECT * FROM photo.v26_region_descriptor_node(%s,%s,%s,%s,%s)',(tenant,run,selected_asset,principal,auth_session)).fetchall()
            finally:
                db.execute('RESET ROLE')
    from uuid import UUID
    assert read() == [(UUID(project),task,node)]
    assert read(principal=uuid4()) == []
    assert read(auth_session=uuid4()) == []
    assert read(selected_asset=uuid4()) == []
    for sql,args in [
        ('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(session,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(params,)),
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(params,)),
        ('DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND artifact_id=%s',(task,chunk)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(chunk,)),
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(manifest,)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(sql,args)
            assert read() == []

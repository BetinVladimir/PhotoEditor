"""Populated SQL authority proof; metadata fixture, not renderer qualification."""
from uuid import uuid4
from copy import deepcopy
from hashlib import sha256
import json
import pytest
from psycopg.types.json import Jsonb
from test_v26_region_source import _region_scope_fixture


@pytest.mark.parametrize('profile_patch,resource_class',[
    ({},'gpu'),
    ({'backend':'cpu'},'gpu'),
    ({'renderer_version':'v2'},'gpu'),
    ({'icc_lock_sha':'f'*64},'gpu'),
    ({'model_lock_sha':'f'*64},'gpu'),
    ({'region_runtime_sha256':'C'*64},'gpu'),
    ({'region_runtime_sha256':None},'gpu'),
    ({},'cpu'),
])
def test_region_seal_scope_binds_saved_node_and_live_authority(db,profile_patch,resource_class):
    tenant,actor,session,plan,style,_,_,_,_=_region_scope_fixture(db)
    snapshot=db.execute("INSERT INTO photo.snapshots(tenant_id,kind,principal_type,principal_id,digest,item_count,expires_at) VALUES(%s,'v2','user',%s,%s,1,now()+interval '1 hour') RETURNING id",(tenant,actor,'a'*64)).fetchone()[0]
    db.execute("INSERT INTO photo.snapshot_items(tenant_id,snapshot_id,ordinal,asset_version_id,artifact_id,sha256,payload) VALUES(%s,%s,0,%s,%s,%s,'{}')",(tenant,snapshot,plan['request']['version_id'],plan['source']['ref']['artifact_id'],plan['source']['ref']['sha256']))
    db.execute('UPDATE photo.snapshots SET expires_at=NULL WHERE id=%s',(snapshot,))
    plan['frozen_recipe'].update(icc_lock_sha='a'*64,model_lock_sha='b'*64,renderer_version='v2.2')
    body=json.dumps(plan,sort_keys=True,separators=(',',':'))
    digest=sha256(body.encode()).hexdigest()
    params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test','region-params','params-v1',%s,%s,'application/json','document',now()) RETURNING id",(tenant,digest,len(body.encode()))).fetchone()[0]
    task=uuid4()
    profile=dict(renderer_version='v2.2',backend='cuda',icc_lock_sha='a'*64,model_lock_sha='b'*64,region_runtime_sha256='c'*64)
    profile.update(profile_patch)
    node=dict(task_id=str(task),params=dict(artifact_id=str(params),sha256=digest),params_jcs=body,input_digest='d'*64,profile=profile)
    config=dict(project_id=str(plan['project_id']),v26_region=dict(auth_session_id=str(session),nodes=[node]))
    config_sha=sha256(json.dumps(config,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy,graph_sealed) VALUES(%s,'v2','draft',%s,%s,%s,%s,'fail_fast',false) RETURNING id",(tenant,snapshot,Jsonb(config),config_sha,actor)).fetchone()[0]
    db.execute("INSERT INTO photo.v2_workflows(tenant_id,project_id,run_id,input_snapshot_id,workflow_type,renderer_version,policy_sha) VALUES(%s,%s,%s,%s,'preview','v2.2',%s)",(tenant,plan['project_id'],run,snapshot,'e'*64))
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile) VALUES(%s,%s,%s,'region.preview:0','region.preview','1',%s,'[]',%s,%s,%s)",(task,tenant,run,resource_class,params,'d'*64,Jsonb(profile)))
    def check(candidate=node,principal=actor,auth_session=session):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_api')
            try:return db.execute('SELECT photo.v26_region_seal_scope(%s,%s,%s,%s,%s)',(tenant,run,principal,auth_session,Jsonb(candidate))).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert check() is False
    with db.transaction(force_rollback=True):
        # Qualification is deliberately outside this SQL authority fixture.
        db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='region.preview' AND version='1'")
        if profile_patch or resource_class!='gpu':
            # Candidate equals immutable stored config and task profile. Thus
            # rejection proves SQL runtime checks, not just node equality.
            assert check() is False
            return
        assert check() is True
        for key,value in [('task_id',str(uuid4())),('input_digest','f'*64),('params_jcs',body+' '),('profile',dict(backend='cpu'))]:
            changed=deepcopy(node);changed[key]=value
            assert check(changed) is False
        assert check(principal=uuid4()) is False
        assert check(auth_session=uuid4()) is False
        for query,args in [
            ('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',(session,)),
            ('UPDATE photo.styles SET archived_at=now() WHERE id=%s',(style,)),
            ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
            ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,)),
            ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(params,)),
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(params,)),
        ]:
            with db.transaction(force_rollback=True):
                db.execute(query,args)
                assert check() is False

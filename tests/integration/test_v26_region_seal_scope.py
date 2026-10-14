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
    ({'region_qualification':None},'gpu'),
    ({'region_qualification':'omit'},'gpu'),
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
    qualify_region_profile(db,tenant,actor,plan['project_id'],profile)
    profile.update(profile_patch)
    if profile.get("region_qualification")=="omit":profile.pop("region_qualification")
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
    def direct_seal():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.seal_v2_graph(%s,%s)',(tenant,run)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    def scheduler_scope():
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.v26_region_seal_scope_scheduler(%s,%s,%s,%s,%s)',(tenant,run,actor,session,Jsonb(node))).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert check() is False
    assert scheduler_scope() is False
    assert direct_seal() is False
    with db.transaction(force_rollback=True):
        # Controlled imported qualification metadata; no physical GPU measurement.
        db.execute("UPDATE photo.operation_registry SET v2_enabled=true WHERE operation='region.preview' AND version='1'")
        if profile_patch or resource_class!='gpu':
            # Candidate equals immutable stored config and task profile. Thus
            # rejection proves SQL runtime checks, not just node equality.
            assert check() is False
            assert direct_seal() is False
            return
        assert check() is True
        assert scheduler_scope() is True
        for key,value in [('task_id',str(uuid4())),('input_digest','f'*64),('params_jcs',body+' '),('profile',dict(backend='cpu'))]:
            changed=deepcopy(node);changed[key]=value
            assert check(changed) is False
        assert check(principal=uuid4()) is False
        assert check(auth_session=uuid4()) is False
        for query,args in [
            ("UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE operation='region.preview' AND issuer_id=%s",(actor,)),
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


def qualify_region_profile(db,tenant,actor,project,profile):
    """Populate trusted report metadata without claiming native qualification."""
    model,runtime=profile['model_lock_sha'],profile['region_runtime_sha256']
    db.execute('INSERT INTO photo.v2_qualification_operators(operator_id,enabled) VALUES(%s,true)',(actor,))
    db.execute("INSERT INTO photo.v2_qualification_policies(operation,operation_version,model_lock_sha,runtime_lock_sha,backend,device_profile_sha,dataset_sha,issuer_id,required_checks,test_fixture_shas,calibration_shas,evidence_shas,license_cleared,enabled,admission_enabled,region_icc_lock_sha,region_execution_policy_sha) VALUES('region.preview','1',%s,%s,'cuda',%s,%s,%s,ARRAY['quality'],ARRAY[%s],ARRAY[%s],ARRAY[%s],true,true,true,%s,%s)",
        (model,runtime,'4'*64,'5'*64,actor,'6'*64,'1'*64,'f'*64,profile['icc_lock_sha'],'e'*64))
    report=dict(schema_version=1,operation='region.preview',version='1',model_lock_sha=model,
        runtime_lock_sha=runtime,device_profile_sha='4'*64,dataset_sha='5'*64,input_fixture_shas=['6'*64],
        checks=[dict(id='quality',passed=True,evidence_sha='f'*64)],
        metrics=dict(p50_ms=10,p95_ms=20,peak_vram_bytes=300,peak_rss_bytes=100,scratch_bytes=200),
        status='passed',issuer_id=str(actor),created_at='2026-09-01T00:00:00Z')
    body=json.dumps(report,sort_keys=True,separators=(',',':'))
    digest=sha256(body.encode()).hexdigest()
    artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,verified_at) VALUES(%s,'test','region/qualification','v1',%s,%s,'application/json','ready',now()) RETURNING id",(tenant,digest,len(body))).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_maintenance')
    try:db.execute('SELECT photo.import_v2_qualification(%s,%s,%s,%s,%s,%s,%s)',(tenant,project,artifact,digest,actor,'cuda',body))
    finally:db.execute('RESET ROLE')
    profile['resource_estimate']=dict(pixel_count=1000000,ram_bytes=3*1024**3,scratch_bytes=24*1024**2,vram_bytes=300,output_bytes=24*1024**2)
    profile['region_qualification']=dict(catalog_sha='1'*64,calibration_sha='2'*64,workload_sha='3'*64,evidence_sha='f'*64,
        qualification=dict(runtime_lock_sha=runtime,model_lock_sha=model,icc_lock_sha=profile['icc_lock_sha'],
            device_profile_sha='4'*64,dataset_sha='5'*64,policy_sha='e'*64,backend='cuda'),
        sample=dict(width=1000,height=1000,operator_peak=dict(ram_bytes=100,scratch_bytes=200,vram_bytes=300,output_bytes=400)))


def test_generic_region_seal_and_adoption_predecessors_are_private(db):
    for signature in (
        'photo.seal_v2_graph_pre_region_qualification(uuid,uuid)',
        'photo.v26_region_seal_scope_pre_qualification(uuid,uuid,uuid,uuid,jsonb)',
        'photo.adopt_v2_plan_pre_region_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text)',
    ):
        for role in ('photo_api','photo_scheduler','photo_worker','photo_gateway'):
            assert db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()==(False,)

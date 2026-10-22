"""Authored live canonical SQL inventory scenarios; no physical qualification.

This fixture constructs real source/style/session authority and a canonical
producer/attempt/result. Its synthetic JSON artifact metadata tests SQL fences
only; worker physical/schema/replay scenarios provide the separate boundaries.
"""
from copy import deepcopy
from hashlib import sha256
import json
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_v26_region_source import _region_scope_fixture


def execution_fixture(db,operation='image.color'):
    tenant,actor,session,plan,style,recipe,base_run,_,_=_region_scope_fixture(db)
    # The reused recipe fixture starts with legacy role=source. Canonical image
    # ancestry deliberately requires a WorkingImage, so seed a new accepted
    # version rather than mutating immutable artifact metadata.
    source_id=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'source-v1',%s,64,'application/json','working_image',now()) RETURNING id",(tenant,str(uuid4()),'a'*64)).fetchone()[0]
    source=dict(artifact_id=str(source_id),sha256='a'*64)
    source_version=db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state) VALUES(%s,%s,%s,%s,'{}','accepted') RETURNING id",(tenant,plan['asset_id'],source_id,'a'*64)).fetchone()[0]
    plan['source']['ref']=source;plan['request']['version_id']=str(source_version)
    location=db.execute('SELECT bucket,object_key,object_version,bytes FROM photo.artifacts WHERE id=%s',(source_id,)).fetchone()
    plan['source'].update(zip(('bucket','object_key','object_version','bytes'),location))
    plan['frozen_recipe']['source'].update(source=source,asset_version_id=str(source_version))
    style_ref=plan['frozen_recipe']['base_style']
    snapshot=db.execute('SELECT input_snapshot_id FROM photo.runs WHERE id=%s',(base_run,)).fetchone()[0]
    config=dict(project_id=plan['project_id'],style_artifact_id=style_ref['artifact_id'],style_hash=style_ref['sha256'])
    config_raw=json.dumps(config,sort_keys=True,separators=(',',':'))
    run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'apply','completed',%s,%s,%s,%s,true) RETURNING id",(tenant,snapshot,Jsonb(config),sha256(config_raw.encode()).hexdigest(),actor)).fetchone()[0]
    params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',now()) RETURNING id",(tenant,str(uuid4()),'c'*64)).fetchone()[0]
    task=uuid4()
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'color/0',%s,'1','cpu','ready',%s,%s,%s,'{\"backend\":\"cpu\"}',1,1)",(task,tenant,run,operation,Jsonb([source]),params,'d'*64))
    db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cpu','{}')",(tenant,task,uuid4()))
    output=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,'application/json','working_image',now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),'e'*64,task)).fetchone()[0]
    target=dict(artifact_id=str(output),sha256='e'*64)
    result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,task,Jsonb([target]),'f'*64)).fetchone()[0]
    db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,task))
    db.execute("INSERT INTO photo.asset_versions(tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state,created_by_run_id) VALUES(%s,%s,%s,%s,'{}','accepted',%s)",(tenant,plan['asset_id'],output,'e'*64,run))
    stage=dict(operation=operation,operation_version=1,task_id=str(task),result_id=str(result),attempt=1,fencing_token=1,
        params=dict(artifact_id=str(params),sha256='c'*64),source=source,output=target)
    plan['frozen_recipe']['source']['canvas_hash']='a'*64
    plan['execution']=dict(schema_version=1,source=source,source_canvas_hash='a'*64,style=style_ref,stages=[stage])
    plan['inputs']=[dict(ref=source,contract='WorkingImage'),dict(ref=target,contract='WorkingImage'),dict(ref=stage['params'],contract='Params')]
    def scope(value=plan):
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            return db.execute('SELECT photo.v26_region_execution_scope(%s,%s,%s,%s)',(tenant,actor,session,Jsonb(value))).fetchone()[0]
    return tenant,task,run,output,session,plan,scope


def test_current_canonical_execution_inventory_is_positive_then_fails_live_lifecycle(db):
    tenant,task,run,output,session,plan,scope=execution_fixture(db)
    assert scope() is True
    for query,identity in [
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',task),
        ("UPDATE photo.runs SET status='failed' WHERE id=%s",run),
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",output),
        ('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',session),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,(identity,));assert scope() is False
    for field,value in [('result_id',str(uuid4())),('fencing_token',2),('attempt',2)]:
        changed=deepcopy(plan);changed['execution']['stages'][0][field]=value
        assert scope(changed) is False
    changed=deepcopy(plan);changed['inputs']=changed['inputs'][:-1]
    assert scope(changed) is False


def test_execution_dependency_readers_remain_narrow_roles(db):
    signature='photo.v26_region_execution_background(uuid,uuid,uuid,uuid,text,uuid,text,uuid,uuid,uuid,uuid,uuid,text)'
    for role in ('photo_worker','photo_scheduler','photo_gateway','photo_portal'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(\'photo_api\',%s,\'EXECUTE\')',(signature,)).fetchone()[0]


def outpaint_plan_fixture(db,variant='exact'):
    tenant,task,run,output,session,plan,_=execution_fixture(db,'image.outpaint')
    actor=db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    def producer(run_id,tag):
        producer_id=uuid4()
        params=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',now()) RETURNING id",(tenant,str(uuid4()),tag*64)).fetchone()[0]
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,%s,'composition.plan','1','cpu','ready',%s,%s,%s,'{\"backend\":\"cpu\"}',1,1)",(producer_id,tenant,run_id,str(producer_id),Jsonb([plan['source']['ref']]),params,'d'*64))
        db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,1,1,%s,now(),now(),'cpu','{}')",(tenant,producer_id,uuid4()))
        artifact=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,'test',%s,'v1',%s,64,'application/json','composition_plan',now(),%s,1,1) RETURNING id",(tenant,str(uuid4()),tag*64,producer_id)).fetchone()[0]
        ref=dict(artifact_id=str(artifact),sha256=tag*64)
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tenant,producer_id,Jsonb([ref]),tag*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer_id))
        return producer_id,ref
    foreign_run=run
    if variant=='foreign_run':
        foreign_run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) SELECT tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed FROM photo.runs WHERE id=%s RETURNING id",(run,)).fetchone()[0]
    selected,ref=producer(foreign_run,'b')
    prerequisites=[selected]
    if variant=='sibling':
        selected,ref=producer(run,'9');prerequisites=[uuid4()]
    if variant=='ambiguous':prerequisites.append(producer(run,'8')[0])
    if variant=='stale_attempt':db.execute('UPDATE photo.tasks SET attempt=2 WHERE id=%s',(selected,))
    phase='generate' if variant=='wrong_phase' else 'composition_render'
    db.execute("INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,prerequisites,params_sha256,params) VALUES(%s,%s,%s,%s,0,%s,%s,%s,'composition_render/0','image.outpaint',%s,%s,%s,'{}')",(task,tenant,run,phase,uuid4(),plan['asset_id'],plan['request']['version_id'],Jsonb([plan['source']['ref']]),prerequisites,'c'*64))
    def resolve():
        with db.transaction():
            db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
            return db.execute('SELECT photo.v26_region_execution_outpaint_plan(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',(tenant,plan['asset_id'],plan['request']['version_id'],plan['source']['ref']['artifact_id'],plan['source']['ref']['sha256'],plan['frozen_recipe']['base_style']['artifact_id'],plan['frozen_recipe']['base_style']['sha256'],actor,session,run,task)).fetchone()[0]
    return resolve,ref


def test_outpaint_resolves_exact_same_run_canonical_prerequisite(db):
    resolve,ref=outpaint_plan_fixture(db)
    assert resolve()==ref


@pytest.mark.parametrize('variant',['foreign_run','sibling','ambiguous','stale_attempt','wrong_phase'])
def test_outpaint_refuses_unbound_or_noncanonical_plan(db,variant):
    resolve,_=outpaint_plan_fixture(db,variant)
    assert resolve() is None


def test_region_apply_phase_inventory_has_no_application_update_privilege(db):
    for role in ('photo_fn_owner','photo_api','photo_scheduler','photo_worker'):
        assert not db.execute("SELECT has_table_privilege(%s,'photo.apply_nodes','UPDATE')",(role,)).fetchone()[0]

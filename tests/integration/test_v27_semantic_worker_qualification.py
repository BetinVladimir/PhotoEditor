"""Lease-scoped worker pin metadata, not physical GPU/report/source evidence."""
from uuid import uuid4
from types import SimpleNamespace
from contextlib import contextmanager
import pytest
import psycopg
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin,semantic_runtime_authority
from photo_worker.runtime.database import TaskDatabase
from test_v27_semantic_final_producer_gate import fixture


@pytest.mark.parametrize("calibration_input",["pinned","alternate","absent"])
def test_semantic_worker_qualification_requires_live_registered_lease(db,calibration_input):
    tid,_,_,task,_,_,scope=fixture(db,snapshot_authority=True,qualification=True,calibration_input=calibration_input,protection_pins=True)
    run,params,digest=db.execute('SELECT run_id,params_artifact_id,input_digest FROM photo.tasks WHERE id=%s',(task,)).fetchone()
    worker,node=uuid4(),'semantic-pin-'+str(uuid4())
    db.execute("INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,12000000000,2000000000)",(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"gpu\"]','[\"cuda\"]',%s,now(),'{}',session_user,%s,true,'{\"semantic.transfer\":[\"cuda\"]}','[\"v2.2\"]')",(tid,worker,'a'*64,node))
    db.execute("UPDATE photo.tasks SET state='running',worker_id=%s,lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '120 seconds' WHERE id=%s",(worker,task))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'gpu1','gpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')",(node,tid,task))
    args=(tid,run,task,1,1,worker,params,'f'*64,digest)
    def read(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            try:return db.execute('SELECT photo.v27_semantic_worker_qualification('+','.join(['%s']*9)+')',values).fetchone()[0]
            finally:db.execute('RESET ROLE')
    if calibration_input!='pinned':
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()[0]
        assert read() is None
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            try:assert db.execute('SELECT photo.v27_semantic_worker_report_evidence('+','.join(['%s']*9)+')',args).fetchone()[0] is None
            finally:db.execute('RESET ROLE')
        return
    assert read()==scope['qualification']
    def source_ready(version=scope['version'],source=scope['source'],sha='5'*64,target=True):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            try:return db.execute('SELECT photo.v27_semantic_worker_source_ready('+','.join(['%s']*13)+')',(*args,version,source,sha,target)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert source_ready() and source_ready(target=False)
    assert not source_ready(version=uuid4()) and not source_ready(source=uuid4()) and not source_ready(sha='0'*64)
    for statement,values in [
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['original'],)),
        ("UPDATE photo.asset_versions SET review_state='rejected' WHERE id=%s",(scope['version'],)),
    ]:
        with db.transaction(force_rollback=True):db.execute(statement,values);assert not source_ready()

    for index,value in [(0,uuid4()),(1,uuid4()),(2,uuid4()),(3,2),(4,2),(5,uuid4()),(6,uuid4()),(7,'0'*64),(8,'0'*64)]:
        changed=list(args);changed[index]=value
        assert read(tuple(changed)) is None
    pin=scope['qualification']
    for statement,values in [
        ('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(worker,)),
        ("UPDATE photo.worker_registrations SET db_principal='other_login' WHERE worker_id=%s",(worker,)),
        ("UPDATE photo.worker_registrations SET qualified_operations='{}' WHERE worker_id=%s",(worker,)),
        ("UPDATE photo.worker_registrations SET v2_renderer_versions='[]' WHERE worker_id=%s",(worker,)),
        ("UPDATE photo.worker_registrations SET backends='[]' WHERE worker_id=%s",(worker,)),
        ("UPDATE photo.worker_registrations SET resource_classes='[]' WHERE worker_id=%s",(worker,)),
        ("UPDATE photo.runs SET status='draft' WHERE id=%s",(run,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(run,)),
        ("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{backend}','\"cpu\"') WHERE id=%s",(task,)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(params,)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(params,)),
        ("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(task,)),
        ("UPDATE photo.tasks SET deadline=clock_timestamp()-interval '1 second' WHERE id=%s",(task,)),
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(task,)),
        ("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s",(task,)),
        ('UPDATE photo.resource_slots SET fencing_token=2 WHERE task_id=%s',(task,)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,values)
            assert read() is None, statement
    assert read()==pin
    for field,value in [('renderer_version','v2.1')]:
        with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState,match='V2_TASK_INPUTS_IMMUTABLE'):
            with db.transaction():
                db.execute("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,%s,%s::jsonb) WHERE id=%s",([field],'"'+value+'"',task))
    assert read()==pin
    @contextmanager
    def connection():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            try:yield db
            finally:db.execute('RESET ROLE')
    transport=TaskDatabase('fixture');transport._connect=connection
    lease=SimpleNamespace(command=SimpleNamespace(tenant_id=tid,run_id=run,task_id=task,attempt=1,params=SimpleNamespace(artifact_id=params,sha256='f'*64)),fence=1,worker_id=worker,input_digest=digest)
    assert transport.semantic_qualification(lease)==pin
    assert transport.semantic_protection_pins(lease)==scope['protection']
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['protection']['source_protection']['artifact_id'],))
        assert transport.semantic_protection_pins(lease) is None
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(scope['protection']['target_protection']['artifact_id'],))
        assert transport.semantic_protection_pins(lease) is None
    binding=SimpleNamespace(asset_version_id=scope['version'],source=SimpleNamespace(artifact_id=scope['source'],sha256='5'*64))
    assert transport.semantic_source_ready(lease,binding,target=True)
    assert transport.semantic_source_ready(lease,binding,target=False)
    protection=SimpleNamespace(**scope['protection']['source_protection'])
    assert not transport.semantic_protection_origin(lease,binding,protection,protection,target=False)
    with db.transaction(force_rollback=True):
        from psycopg.types.json import Jsonb
        producer=uuid4()
        mask_run=db.execute("INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed) VALUES(%s,'preview','running',%s,%s,%s,%s,true) RETURNING id",(tid,scope['snapshot'],Jsonb({'project_id':str(scope['project'])}),'b'*64,scope['actor'])).fetchone()[0]
        db.execute("INSERT INTO photo.interactive_tasks(tenant_id,run_id,task_id,kind,ordinal,operation,source_artifact_id,declared_inputs,params,params_sha256,backend,runtime_lock_id,state) VALUES(%s,%s,%s,'mask_manual',0,'mask.rasterize',%s,'[]','{}',%s,'cpu',%s,'complete')",(tid,mask_run,producer,scope['source'],'c'*64,'d'*64))
        db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token) VALUES(%s,%s,%s,'mask/00','mask.rasterize','1','cpu','ready','[]',%s,%s,'{}',1,1)",(producer,tid,mask_run,scope['source'],'e'*64))
        child=SimpleNamespace(artifact_id=uuid4(),sha256='4'*64)
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at,output_role,created_by_task_id,produced_attempt,produced_fence) VALUES(%s,%s,'fixture',%s,%s,128,'application/octet-stream','v1',clock_timestamp(),'mask_protected',%s,1,1)",(child.artifact_id,tid,str(child.artifact_id),child.sha256,producer))
        db.execute("UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1,output_role='mask_protected_ref' WHERE id=%s",(producer,protection.artifact_id))
        outputs=[dict(artifact_id=str(value.artifact_id),sha256=value.sha256) for value in (protection,child)]
        result=db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) VALUES(%s,%s,1,1,%s,'{}',%s) RETURNING id",(tid,producer,Jsonb(outputs),'e'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s",(result,producer))
        assert transport.semantic_protection_origin(lease,binding,protection,child,target=False)
        assert not transport.semantic_protection_origin(lease,binding,protection,child,target=True)
        assert not transport.semantic_protection_origin(lease,binding,protection,SimpleNamespace(artifact_id=uuid4(),sha256=child.sha256),target=False)
        for statement,values in [
            ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(child.artifact_id,)),
            ('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(child.artifact_id,)),
            ("UPDATE photo.artifacts SET output_role='mask_target' WHERE id=%s",(child.artifact_id,)),
            ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(producer,)),
            ('UPDATE photo.interactive_tasks SET source_artifact_id=%s WHERE task_id=%s',(scope['original'],producer)),
        ]:
            with db.transaction(force_rollback=True):
                db.execute(statement,values)
                assert not transport.semantic_protection_origin(lease,binding,protection,child,target=False)

    evidence=transport.semantic_report_evidence(lease)
    assert evidence['pin']==pin
    assert evidence['report']['artifact_id']==pin['report']['artifact_id']
    assert evidence['report']['sha256']==pin['report']['sha256']
    assert evidence['report']['object_version']=='pinned-v1'
    from photo_worker.contracts.base import canonical_hash
    assert canonical_hash(evidence['policy'])==pin['policy_sha']
    frozen=SemanticQualificationPin.from_value(pin)
    verify=semantic_runtime_authority(lambda:transport.semantic_qualification(lease),frozen_pin=frozen,
        model_lock_sha=pin['model_lock_sha'],runtime_lock_sha=pin['runtime_lock_sha'],
        device_profile_sha=pin['device_profile_sha'],dataset_sha=pin['dataset_sha'])
    assert verify()==frozen
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        assert transport.semantic_report_evidence(lease) is None
        with pytest.raises(AdoptionInvalid):verify()
    assert verify()==frozen
    for role in ('photo_api','photo_scheduler','public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_worker_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,uuid,uuid,text,boolean,uuid,text,uuid,text)','EXECUTE')",(role,)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_worker_protection_pins(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text)','EXECUTE')",(role,)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_worker_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text)','EXECUTE')",(role,)).fetchone()[0]
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_worker_qualification(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text)','EXECUTE')",(role,)).fetchone()[0]

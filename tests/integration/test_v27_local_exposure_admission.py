from hashlib import sha256
from uuid import uuid4
import pytest


def test_local_exposure_authority_is_worker_only_and_denies_unowned_task(db):
    signature='photo.admit_semantic_local_exposure(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for role in ('photo_worker','photo_api','photo_scheduler','photo_gateway'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is (role=='photo_worker')
    raw='{"schema_version":1,"kind":"local_exposure"}'
    values=(uuid4(),uuid4(),uuid4(),1,1,uuid4(),uuid4(),sha256(raw.encode()).hexdigest(),raw,'a'*64)
    def admit(args):
        return db.execute('SELECT photo.admit_semantic_local_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',args).fetchone()[0]
    signature='photo.semantic_local_exposure_protection_origin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text,uuid,text,uuid,text)'
    for role in ('photo_api','photo_worker','photo_scheduler','photo_gateway'):
        assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',(role,signature)).fetchone()[0] is (role=='photo_worker')
    assert db.execute('SELECT photo.semantic_local_exposure_protection_origin('+','.join(['%s']*14)+')',(*values,uuid4(),'a'*64,uuid4(),'b'*64)).fetchone()[0] is False
    assert admit(values) is False
    for index,value in ((3,0),(4,0),(7,'0'*64),(8,None),(8,'x'*1048577)):
        changed=list(values);changed[index]=value
        assert admit(changed) is False


def test_live_worker_child_admission_and_fence_revocation(db):
    from psycopg.types.json import Jsonb
    from test_v27_semantic_final_producer_gate import fixture
    from photo_worker.contracts.base import canonical_bytes
    tid,assembly,_,_,_,candidate,scope=fixture(db,snapshot_authority=True,protection_pins=True)
    parent=db.execute('SELECT run_id FROM photo.tasks WHERE id=%s',(assembly,)).fetchone()[0]
    workflow=db.execute('SELECT id FROM photo.v2_workflows WHERE run_id=%s',(parent,)).fetchone()[0]
    intent=db.execute('INSERT INTO photo.semantic_intents(tenant_id,project_id,workflow_id,reference_version_id,snapshot_id,intent_artifact_id,intent_sha) VALUES(%s,%s,%s,%s,%s,%s,%s) RETURNING id',(tid,scope['project'],workflow,scope['version'],scope['snapshot'],scope['source'],'5'*64)).fetchone()[0]
    target=db.execute("INSERT INTO photo.semantic_targets(tenant_id,project_id,intent_id,asset_version_id,candidate_artifact_id,state) VALUES(%s,%s,%s,%s,%s,'proposed') RETURNING id",(tid,scope['project'],intent,scope['version'],candidate)).fetchone()[0]
    child,task,params,worker=uuid4(),uuid4(),uuid4(),uuid4()
    candidate_sha=db.execute('SELECT sha256 FROM photo.artifacts WHERE id=%s',(candidate,)).fetchone()[0]
    def ref(aid,digest):return dict(artifact_id=str(aid),sha256=digest)
    edit=dict(schema_version=1,target_id=str(target),source=dict(asset_version_id=str(scope['version']),source=ref(scope['source'],'5'*64),canvas_hash='b'*64),candidate=ref(candidate,candidate_sha),intent=ref(scope['source'],'5'*64),mask=ref(uuid4(),'a'*64),transform=ref(uuid4(),'a'*64),kind='local_exposure',ev=.25,strength=.5,prompt=None,preserve_persons=True)
    for pin in (edit['mask'],edit['transform']):
        db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,2,'application/json','v1',clock_timestamp())",(pin['artifact_id'],tid,pin['artifact_id'],pin['sha256']))
    raw=canonical_bytes(edit).decode();digest=sha256(raw.encode()).hexdigest()
    config=dict(project_id=str(scope['project']),approved_semantic=dict(nodes=[dict(task_id=str(task),params_artifact_id=str(params),params_sha=digest,target_id=str(target))]))
    db.execute("INSERT INTO photo.runs(id,tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,failure_policy) VALUES(%s,%s,'apply','running',%s,%s,%s,%s,'fail_fast')",(child,tid,scope['snapshot'],Jsonb(config),'b'*64,scope['actor']))
    batch=db.execute("INSERT INTO photo.v2_review_batches(tenant_id,project_id,workflow_id,decisions_artifact_id,actor_id,action,child_run_id) VALUES(%s,%s,%s,%s,%s,'apply',%s) RETURNING id",(tid,scope['project'],workflow,scope['source'],scope['actor'],child)).fetchone()[0]
    db.execute("UPDATE photo.semantic_targets SET state='queued',batch_id=%s,child_run_id=%s WHERE id=%s",(batch,child,target))
    db.execute("INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,media_type,object_version,verified_at) VALUES(%s,%s,'fixture',%s,%s,%s,'application/json','v1',clock_timestamp())",(params,tid,str(params),digest,len(raw.encode())))
    profile=dict(backend='cpu',renderer_version='v2.2',model_lock_id='a'*64)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile,attempt,fencing_token,state,worker_id,lease_until,deadline) VALUES(%s,%s,%s,'semantic.local_exposure:0','semantic.local_exposure','1','cpu','[]',%s,%s,%s,1,1,'running',%s,clock_timestamp()+interval '60 seconds',clock_timestamp()+interval '120 seconds')",(task,tid,child,params,'b'*64,Jsonb(profile),worker))
    node='exposure-'+str(uuid4())
    db.execute('INSERT INTO photo.resource_nodes(node_id,ram_budget_bytes,vram_budget_bytes,scratch_budget_bytes) VALUES(%s,4000000000,0,2000000000)',(node,))
    db.execute("INSERT INTO photo.worker_registrations(tenant_id,worker_id,resource_classes,backends,lock_id,last_seen,capacity,db_principal,node_id,enabled,qualified_operations,v2_renderer_versions) VALUES(%s,%s,'[\"cpu\"]','[\"cpu\"]',%s,now(),'{}',session_user,%s,true,'{\"semantic.local_exposure\":[\"cpu\"]}','[\"v2.2\"]')",(tid,worker,'a'*64,node))
    db.execute("INSERT INTO photo.resource_slots(node_id,slot_name,resource_class,capacity,tenant_id,task_id,fencing_token,lease_until) VALUES(%s,'cpu1','cpu','{}',%s,%s,1,clock_timestamp()+interval '60 seconds')",(node,tid,task))
    args=(tid,child,task,1,1,worker,params,digest,raw,'b'*64)
    def admit(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            result=db.execute('SELECT photo.admit_semantic_local_exposure(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)',values).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert admit() is True
    def report(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            result=db.execute('SELECT photo.v27_exposure_worker_report_evidence('
                + ','.join(['%s']*10) + ')',values).fetchone()[0]
            db.execute('RESET ROLE')
            return result
    assert report() is None  # A live lease alone is not qualification.
    from semantic_qualification_fixture import frozen_qualification
    qualification=frozen_qualification(db,tid,scope['project'],scope['actor'],
        operation='semantic.local_exposure',backend='cpu')
    profile.update(qualification=qualification, **{key:qualification[key] for key in
        ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')})
    db.execute('UPDATE photo.tasks SET execution_profile=%s WHERE id=%s',(Jsonb(profile),task))
    evidence=report()
    def protection(values=args):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_worker')
            result=db.execute('SELECT * FROM photo.v27_exposure_worker_protection_pin('
                + ','.join(['%s']*10)+')',values).fetchall()
            db.execute('RESET ROLE');return result
    expected_protection=scope['protection']['target_protection']
    from uuid import UUID
    assert protection()==[(UUID(expected_protection['artifact_id']),expected_protection['sha256'])]
    assert evidence['pin']==qualification
    assert evidence['policy']['operation']=='semantic.local_exposure'
    assert evidence['policy']['backend']=='cpu'
    assert evidence['report']['artifact_id']==qualification['report']['artifact_id']
    assert evidence['report']['object_version'] not in ('','null',None)
    for statement,values in (
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(qualification['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(qualification['report']['artifact_id'],)),
        ('UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(qualification['calibration']['artifact_id'],)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(statement,values)
            assert report() is None
    import psycopg
    with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState):
        with db.transaction(force_rollback=True):
            db.execute("UPDATE photo.model_qualifications SET status='failed' WHERE id=%s",
                (qualification['qualification_id'],))
    assert report()==evidence
    for key in ('mask','transform'):
        for mutation in ('verified_at=NULL',"expires_at=clock_timestamp()-interval '1 second'"):
            with db.transaction(force_rollback=True):
                db.execute('UPDATE photo.artifacts SET '+mutation+' WHERE id=%s',(edit[key]['artifact_id'],))
                assert admit() is False
    for index in (0,1,2,3,4,5,6,7,9):
        changed=list(args);changed[index]=2 if index in (3,4) else '0'*64 if index in (7,9) else uuid4()
        assert admit(tuple(changed)) is False
        assert report(tuple(changed)) is None
        assert protection(tuple(changed))==[]
    for query,values in (
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(task,)),
        ("UPDATE photo.tasks SET lease_until=clock_timestamp()-interval '1 second' WHERE id=%s",(task,)),
        ('UPDATE photo.resource_slots SET fencing_token=2 WHERE task_id=%s',(task,)),
        ('UPDATE photo.worker_registrations SET enabled=false WHERE worker_id=%s',(worker,)),
        ("UPDATE photo.worker_registrations SET db_principal='another_principal' WHERE worker_id=%s",(worker,)),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(child,)),
    ):
        with db.transaction(force_rollback=True):
            db.execute(query,values)
            assert admit() is False
            assert report() is None
            assert protection()==[]
    assert admit() is True
    assert report()==evidence


def test_exposure_worker_report_evidence_is_worker_only_and_denies_unowned_scope(db):
    signature = 'photo.v27_exposure_worker_report_evidence(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for role in ('photo_worker', 'photo_api', 'photo_scheduler', 'photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
            (role, signature)).fetchone()[0] is (role == 'photo_worker')
    pin_signature='photo.v27_exposure_worker_protection_pin(uuid,uuid,uuid,integer,bigint,uuid,uuid,text,text,text)'
    for role in ('photo_worker','photo_api','photo_scheduler','photo_gateway'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')",
            (role,pin_signature)).fetchone()[0] is (role=='photo_worker')
    raw = '{"schema_version":1,"kind":"local_exposure"}'
    args = (uuid4(), uuid4(), uuid4(), 1, 1, uuid4(), uuid4(),
        sha256(raw.encode()).hexdigest(), raw, 'a'*64)
    assert db.execute('SELECT photo.v27_exposure_worker_report_evidence('
        + ','.join(['%s']*10) + ')', args).fetchone()[0] is None

"""Scoped qualification metadata for adoption; not final adoption/GPU evidence."""
from uuid import uuid4
import pytest
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_adoption_qualification_requires_current_result_and_frozen_proof(db):
    tid,assembly,result,producer,producer_result,_,scope=fixture(db,snapshot_authority=True,qualification=True)
    def ready(task=assembly,attempt=1,fence=1,output=result,tenant=tid):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.v27_semantic_adoption_qualification_ready(%s,%s,%s,%s,%s)',(tenant,task,attempt,fence,output)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert ready() and ready(task=producer,output=producer_result)
    assert not ready(task=uuid4()) and not ready(tenant=uuid4())
    assert not ready(attempt=2) and not ready(fence=2) and not ready(output=uuid4())
    pin=scope['qualification']
    for statement,args in [
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(assembly,)),
        ('UPDATE photo.tasks SET fencing_token=2 WHERE id=%s',(assembly,)),
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['changed'] WHERE id=%s",(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
        ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(pin['calibration']['artifact_id'],)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,args);assert not ready()
    assert ready()
    for role in ('photo_api','photo_worker','public'):
        assert not db.execute("SELECT has_function_privilege(%s,'photo.v27_semantic_adoption_qualification_ready(uuid,uuid,integer,bigint,uuid)','EXECUTE')",(role,)).fetchone()[0]


def test_final_semantic_adoption_requires_qualification_and_preserves_replay_counts(db):
    from psycopg.types.json import Jsonb
    tid,task,result,_,_,candidate,scope=fixture(db,snapshot_authority=True,qualification=True)
    root=db.execute('SELECT outputs->0 FROM photo.task_results WHERE id=%s',(result,)).fetchone()[0]
    inventory=[dict(root,purpose='a'*64,ordinal=0),dict(artifact_id=str(candidate),sha256='9'*64,purpose='b'*64,ordinal=1)]
    def adopt():
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:return db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',(tid,task,result,Jsonb(inventory),'a'*64)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    def counts():
        return tuple(db.execute('SELECT count(*) FROM photo.'+table+' WHERE tenant_id=%s',(tid,)).fetchone()[0] for table in ('v2_artifact_links','inbox','run_events','outbox'))
    before=counts();pin=scope['qualification']
    for statement,args in [
        ('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],)),
        ("UPDATE photo.v2_qualification_policies SET required_checks=ARRAY['changed'] WHERE id=%s",(pin['policy_id'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(pin['report']['artifact_id'],)),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(statement,args);assert not adopt();assert counts()==before
    assert adopt()
    accepted=counts();assert accepted[1]==before[1]+1
    assert not adopt() and counts()==accepted
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
        assert not adopt() and counts()==accepted
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.adopt_v2_plan_pre_semantic_qualification(uuid,uuid,integer,bigint,uuid,jsonb,text)','EXECUTE')").fetchone()[0]


@pytest.mark.parametrize('calibration_input',['alternate','absent'])
def test_final_adoption_rejects_missing_or_same_sha_alternate_calibration(db,calibration_input):
    from psycopg.types.json import Jsonb
    tid,task,result,_,_,candidate,_=fixture(db,snapshot_authority=True,qualification=True,calibration_input=calibration_input)
    root=db.execute('SELECT outputs->0 FROM photo.task_results WHERE id=%s',(result,)).fetchone()[0]
    inventory=[dict(root,purpose='a'*64,ordinal=0),dict(artifact_id=str(candidate),sha256='9'*64,purpose='b'*64,ordinal=1)]
    assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()[0]
    before=tuple(db.execute('SELECT count(*) FROM photo.'+table+' WHERE tenant_id=%s',(tid,)).fetchone()[0] for table in ('v2_artifact_links','inbox','run_events','outbox'))
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:assert not db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',(tid,task,result,Jsonb(inventory),'a'*64)).fetchone()[0]
    finally:db.execute('RESET ROLE')
    after=tuple(db.execute('SELECT count(*) FROM photo.'+table+' WHERE tenant_id=%s',(tid,)).fetchone()[0] for table in ('v2_artifact_links','inbox','run_events','outbox'))
    assert after==before

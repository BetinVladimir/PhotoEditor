"""Live catalog/session SQL evidence, not pixel/model or object-store proof."""
from uuid import uuid4
import psycopg
import pytest
from test_v27_semantic_final_producer_gate import fixture


def test_semantic_snapshot_positive_catalog_and_live_revocation(db):
    tid,task,result,_,_,_,scope=fixture(db,snapshot_authority=True)
    def read(tenant=tid,attempt=1,fence=1,result_id=result):
        db.execute('SET LOCAL ROLE photo_scheduler')
        try:return db.execute('SELECT * FROM photo.v27_semantic_assembly_snapshot(%s,%s,%s,%s,%s)',(tenant,task,attempt,fence,result_id)).fetchall()
        finally:db.execute('RESET ROLE')
    expected=[(scope['root'],'8'*64,scope['snapshot'],'7'*64,0,scope['version'],scope['source'],'5'*64)]
    assert read()==expected
    assert read(uuid4())==[]
    assert read(attempt=2)==[]
    assert read(fence=2)==[]
    assert read(result_id=uuid4())==[]
    for query,args in [
        ('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.sessions SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(scope['session'],)),
        ('UPDATE photo.projects SET archived_at=clock_timestamp() WHERE id=%s',(scope['project'],)),
        ('UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s',(scope['asset'],)),
        ('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s',(scope['source'],)),
        ('UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(scope['original'],)),
        ('UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval \'1 second\' WHERE id=%s',(scope['root'],)),
        ("UPDATE photo.memberships SET role='viewer' WHERE tenant_id=%s AND user_id=%s",(tid,scope['actor'])),
    ]:
        with db.transaction(force_rollback=True):
            db.execute(query,args)
            assert read()==[]
            db.execute('SET LOCAL ROLE photo_scheduler')
            try:
                assert not db.execute("SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,'[]',%s)",(tid,task,result,'a'*64)).fetchone()[0]
            finally:db.execute('RESET ROLE')
    assert read()==expected

    for query,args in [
        ('UPDATE photo.snapshots SET principal_id=%s WHERE id=%s',(uuid4(),scope['snapshot'])),
        ('UPDATE photo.snapshots SET item_count=2 WHERE id=%s',(scope['snapshot'],)),
    ]:
        with pytest.raises(psycopg.errors.CheckViolation):
            with db.transaction():db.execute(query,args)
    assert read()==expected

    with db.transaction(force_rollback=True):
        saved=db.execute("DELETE FROM photo.v2_task_inputs WHERE task_id=%s AND artifact_id=%s RETURNING tenant_id,project_id,task_id,artifact_id,sha256,ordinal",(task,scope['root'])).fetchone()
        db.execute("INSERT INTO photo.v2_task_inputs(tenant_id,project_id,task_id,artifact_id,sha256,ordinal,contract) VALUES(%s,%s,%s,%s,%s,%s,'Calibration')",saved)
        assert db.execute('SELECT photo.v2_input_closure_ready(%s)',(task,)).fetchone()[0]
        assert read()==[]
    assert read()==expected


def test_semantic_snapshot_adoption_replay_preserves_accepted_result(db):
    from psycopg.types.json import Jsonb
    tid,task,result,_,_,candidate,scope=fixture(db,snapshot_authority=True)
    root=db.execute('SELECT outputs->0 FROM photo.task_results WHERE id=%s',(result,)).fetchone()[0]
    inventory=[dict(root,purpose='a'*64,ordinal=0),dict(artifact_id=str(candidate),sha256='9'*64,purpose='b'*64,ordinal=1)]
    def adopt(digest='a'*64):
        with db.transaction():
            db.execute('SET LOCAL ROLE photo_scheduler')
            accepted=db.execute('SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,%s,%s)',(tid,task,result,Jsonb(inventory),digest)).fetchone()[0]
            db.execute('RESET ROLE')
            return accepted
    assert adopt()
    db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(scope['session'],))
    assert not adopt()
    with pytest.raises(psycopg.errors.CheckViolation,match='V2_ADOPTION_RESULT_CONFLICT'):
        adopt('f'*64)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v2.task.adopted' AND payload->>'task_id'=%s",(tid,str(task))).fetchone()==(1,)


def test_semantic_snapshot_required_by_final_adoption(db):
    tid,task,result,_,_,_=fixture(db)
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        assert db.execute('SELECT photo.v27_semantic_assembly_inputs_ready(%s,%s,1,1,%s)',(tid,task,result)).fetchone()[0]
        assert not db.execute("SELECT photo.adopt_v2_plan(%s,%s,1,1,%s,'[]',%s)",(tid,task,result,'a'*64)).fetchone()[0]
    finally:db.execute('RESET ROLE')
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND type='v2.task.adopted' AND payload->>'task_id'=%s",(tid,str(task))).fetchone()==(0,)

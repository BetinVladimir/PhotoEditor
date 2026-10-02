"""Recovery identity and lifecycle checks over controlled SQL plan metadata."""
from uuid import uuid4

import pytest
from test_v26_render_recovery_backoff import record, stage_incomplete


def candidates(db):
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        return db.execute('SELECT tenant_id,batch_id FROM photo.next_v26_render_recovery(NULL)').fetchall()
    finally:
        db.execute('RESET ROLE')


@pytest.mark.parametrize('mutation',[
    "UPDATE photo.tasks SET cancel_requested=true WHERE run_id=%s",
    "UPDATE photo.tasks SET state='queued' WHERE run_id=%s",
    "UPDATE photo.tasks SET state='failed',terminal_reason='fixture_failure' WHERE run_id=%s",
    "UPDATE photo.tasks SET input_digest=repeat('c',64) WHERE run_id=%s",
    "UPDATE photo.tasks task SET params_artifact_id=(SELECT artifact.id FROM photo.artifacts artifact WHERE artifact.tenant_id=task.tenant_id AND artifact.id<>task.params_artifact_id LIMIT 1) WHERE task.run_id=%s",
    "UPDATE photo.runs SET cancel_requested=true WHERE id=%s",
    "UPDATE photo.runs SET graph_sealed=true WHERE id=%s",
    "UPDATE photo.runs SET status='queued' WHERE id=%s",
])
def test_only_matching_recoverable_planned_task_causes_backoff(db,mutation):
    tenant,batch=stage_incomplete(db)
    child=db.execute("SELECT id FROM photo.runs WHERE tenant_id=%s AND kind='v2'",(tenant,)).fetchone()[0]
    assert len(candidates(db))==1
    assert record(db,tenant,batch,True)
    db.execute(mutation,(child,))
    # A late failed report cannot defer work no longer owned by input recovery.
    assert record(db,tenant,batch,True)
    assert db.execute('SELECT v26_input_recovery_after,v26_input_recovery_failures FROM photo.runs WHERE id=%s',(batch,)).fetchone()==(None,0)
    assert candidates(db)==[]


def test_unplanned_blocked_render_task_does_not_keep_batch_in_recovery(db):
    tenant,batch=stage_incomplete(db)
    child=db.execute("SELECT id FROM photo.runs WHERE tenant_id=%s AND kind='v2'",(tenant,)).fetchone()[0]
    assert record(db,tenant,batch,True)
    db.execute("INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) SELECT %s,tenant_id,run_id,'render.compose:extra',operation,operation_version,resource_class,'blocked',inputs,params_artifact_id,input_digest,execution_profile FROM photo.tasks WHERE run_id=%s",(uuid4(),child))
    db.execute("UPDATE photo.tasks SET state='failed',terminal_reason='fixture_failure' WHERE run_id=%s AND node_key='render.compose:0'",(child,))
    assert record(db,tenant,batch,True)
    assert db.execute('SELECT v26_input_recovery_after,v26_input_recovery_failures FROM photo.runs WHERE id=%s',(batch,)).fetchone()==(None,0)
    assert candidates(db)==[]
    assert db.execute("SELECT count(*) FROM photo.tasks WHERE run_id=%s AND state='blocked'",(child,)).fetchone()==(1,)


def test_shared_predicate_is_private(db):
    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.v26_render_recovery_pending(uuid,uuid)','EXECUTE')",(role,)).fetchone()==(False,)

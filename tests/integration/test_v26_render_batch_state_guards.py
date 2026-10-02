"""Scheduler-only phase projection and transactional boundaries on all migrations."""
import pytest
import psycopg
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


def test_phase_functions_are_scheduler_only(db):
    for role in ('photo_api','photo_worker','photo_gateway','photo_scheduler'):
        assert db.execute("SELECT has_function_privilege(%s,'photo.sync_v26_render_batch_state()','EXECUTE')",(role,)).fetchone()==(role=='photo_scheduler',)
        assert db.execute("SELECT has_function_privilege(%s,'photo.v26_render_batch_desired_state(uuid,uuid)','EXECUTE')",(role,)).fetchone()==(False,)
    for role in ('photo_api','photo_worker'):
        with pytest.raises(psycopg.errors.InsufficientPrivilege),db.transaction():
            db.execute('SET LOCAL ROLE '+role)
            db.execute('SELECT photo.sync_v26_render_batch_state()')


@pytest.mark.parametrize('state,cancel',[('completed',False),('completed_with_errors',False),('failed',False),('cancelled',True),('cancelling',True),('draft',True)])
def test_phase_sync_preserves_terminal_and_cancellation_state(db,state,cancel):
    tenant,batch=seed_failed_batch(db)
    db.execute('UPDATE photo.runs SET status=%s,cancel_requested=%s WHERE id=%s',(state,cancel,batch))
    db.execute('SET LOCAL ROLE photo_scheduler')
    assert db.execute('SELECT photo.sync_v26_render_batch_state()').fetchone()==(False,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT status,revision,cancel_requested FROM photo.runs WHERE id=%s',(batch,)).fetchone()==(state,1,cancel)
    assert db.execute('SELECT count(*) FROM photo.run_events WHERE run_id=%s',(batch,)).fetchone()==(0,)


@pytest.mark.parametrize('limit',['revision','cursor'])
def test_phase_exhaustion_rolls_back_run_and_event(db,limit):
    tenant,batch=seed_failed_batch(db)
    if limit=='revision':
        db.execute('UPDATE photo.runs SET revision=9007199254740991 WHERE id=%s',(batch,))
    else:
        db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=EXCLUDED.last_cursor',(tenant,))
    before=db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(batch,)).fetchone()
    with pytest.raises(psycopg.errors.CheckViolation,match='EXHAUSTED'),db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        db.execute('SELECT photo.sync_v26_render_batch_state()')
    assert db.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(batch,)).fetchone()==before
    assert db.execute('SELECT count(*) FROM photo.run_events WHERE run_id=%s',(batch,)).fetchone()==(0,)

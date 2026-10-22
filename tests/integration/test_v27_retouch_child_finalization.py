"""Actual Apply finalization/settlement SQL; no synthetic success/adoption marker."""
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_v27_retouch_preview_sql_roles import database, isolated_database_config
from test_v27_shown_retouch_worker_claim import dispatched_copy_fixture


@pytest.mark.parametrize('with_alpha',[False,True])
def test_retouch_success_requires_adoption_then_settles_once(database,with_alpha):
    from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture,adopt,adoption_state
    values,task,commit=successful_copy_metadata_fixture(database,with_alpha=with_alpha)
    tenant=commit[0];child=values[4]
    before=adoption_state(database,tenant,child)
    assert finalize(database,tenant,child)=='pending'
    assert adoption_state(database,tenant,child)==before
    assert adopt(database,commit) is True
    reserved=database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]
    quota=database.execute("SELECT bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,child)).fetchone()[0]
    revision=database.execute('SELECT revision FROM photo.runs WHERE id=%s',(child,)).fetchone()[0]
    assert finalize(database,tenant,child)=='completed'
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(child,)).fetchone()==('completed',revision+1)
    assert database.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(tenant,child)).fetchall()==[('applied',)]
    assert database.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,child)).fetchone()==('consumed',0)
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(reserved-quota,)
    assert database.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='semantic.child.completed'",(tenant,child)).fetchone()==(1,)
    after=adoption_state(database,tenant,child)
    assert finalize(database,tenant,child)=='completed'
    assert adoption_state(database,tenant,child)==after


def finalize(db,tenant,run):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute('SELECT photo.finalize_shown_semantic_apply_child(%s,%s)',(tenant,run)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def test_retouch_finalization_waits_for_worker(database):
    tenant,task,run,worker,command,fence = dispatched_copy_fixture(database)
    before = database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    quota = database.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()
    assert finalize(database,tenant,run)=='pending'
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        assert database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()[0]=='claimed'
        database.execute('RESET ROLE')
    running = database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    assert running==('running',before[1]+(1 if before[0]=='queued' else 0))
    assert finalize(database,tenant,run)=='pending'
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==running
    assert database.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()==quota
    assert database.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(tenant,run)).fetchall()==[('queued',)]
    assert database.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type LIKE 'semantic.child.%%'",(tenant,run)).fetchone()==(0,)


def cancelled_worker_fixture(database):
    tenant,task,run,worker,command,fence = dispatched_copy_fixture(database)
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        claimed = database.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(task,worker,Jsonb(command))).fetchone()
        assert claimed[0]=='claimed'
        database.execute('RESET ROLE')
    database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s',(tenant,run))
    assert finalize(database,tenant,run)=='pending'
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_worker')
        error = dict(code='TASK_CANCELLED',retryable=False,message_key='errors.cancelled',args={})
        assert database.execute('SELECT photo.commit_task_failure(%s,1,%s,%s,%s,%s)',
            (task,claimed[1],worker,Jsonb(error),Jsonb({}))).fetchone()[0]=='committed'
        database.execute('RESET ROLE')
    return tenant,task,run


def test_retouch_cancelled_worker_settles_once_and_releases_quota(database):
    tenant,task,run = cancelled_worker_fixture(database)
    reserved_before = database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()[0]
    quota_bytes = database.execute("SELECT bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()[0]
    assert finalize(database,tenant,run)=='cancelled'
    after = database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()
    assert database.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(tenant,run)).fetchall()==[('cancelled',)]
    assert database.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone()==('released',0)
    assert database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone()==(reserved_before-quota_bytes,)
    assert finalize(database,tenant,run)=='cancelled'
    assert database.execute('SELECT status,revision FROM photo.runs WHERE id=%s',(run,)).fetchone()==after
    assert database.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='semantic.child.cancelled'",(tenant,run)).fetchone()==(1,)
    assert database.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='semantic.child.cancelled' AND payload->>'run_id'=%s",(tenant,str(run))).fetchone()==(1,)


@pytest.mark.parametrize('mode,code',[('cursor','EVENT_CURSOR_EXHAUSTED'),('revision','REVISION_EXHAUSTED')])
def test_retouch_finalization_counter_failure_rolls_back_every_mutation(database,mode,code):
    tenant,task,run = cancelled_worker_fixture(database)
    def snapshot():
        return (
            database.execute('SELECT status,revision,cancel_requested FROM photo.runs WHERE id=%s',(run,)).fetchone(),
            database.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(tenant,)).fetchone(),
            database.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",(tenant,run)).fetchone(),
            database.execute('SELECT id,state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s ORDER BY id',(tenant,run)).fetchall(),
            database.execute('SELECT cursor,type,payload FROM photo.run_events WHERE tenant_id=%s AND run_id=%s ORDER BY cursor',(tenant,run)).fetchall(),
            database.execute('SELECT event_id,routing_key,payload FROM photo.outbox WHERE tenant_id=%s ORDER BY event_id',(tenant,)).fetchall(),
        )
    with database.transaction(force_rollback=True):
        if mode=='cursor':
            database.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor',(tenant,))
        else:
            database.execute('UPDATE photo.runs SET revision=9007199254740991 WHERE tenant_id=%s AND id=%s',(tenant,run))
        before = snapshot()
        with pytest.raises(errors.CheckViolation,match=code):
            finalize(database,tenant,run)
        assert snapshot()==before
        if mode=='cursor':
            assert database.execute('SELECT last_cursor FROM photo.tenant_event_cursors WHERE tenant_id=%s',(tenant,)).fetchone()==(9007199254740991,)
    assert finalize(database,tenant,run)=='cancelled'
    assert database.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',(tenant,run)).fetchall()==[('cancelled',)]

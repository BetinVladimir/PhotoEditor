"""Real SQL final-failure fences; authored only, not executed during coding."""
from concurrent.futures import ThreadPoolExecutor
from time import monotonic, sleep

import psycopg
import pytest
from psycopg.types.json import Jsonb
from test_task_leases import queued_task, claim


def fail(conn, case, fence, metrics=None):
    return conn.execute('SELECT photo.commit_task_failure(%s,1,%s,%s,%s,%s)',
        (case['task'],fence,case['worker'],Jsonb({'code':'TEST_FAILURE','retryable':True}),
         Jsonb(metrics or {}))).fetchone()[0]


def assert_unmodified(conn, case):
    assert conn.execute('SELECT state FROM photo.tasks WHERE id=%s',(case['task'],)).fetchone()==('running',)
    assert conn.execute('SELECT finished_at,error FROM photo.task_attempts WHERE task_id=%s AND attempt=1',(case['task'],)).fetchone()==(None,None)
    assert conn.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(case['tenant'],)).fetchone()==(0,)
    assert conn.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(case['task'],)).fetchone()==(1,)


def test_expired_slot_cannot_commit_failure_with_live_task_lease(db):
    case=queued_task(db)
    fence=claim(db,case)[1]
    db.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '60 seconds' WHERE id=%s",(case['task'],))
    db.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()-interval '1 second' WHERE task_id=%s",(case['task'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert fail(db,case,fence)=='stale'
    db.execute('RESET ROLE')
    assert_unmodified(db,case)


def test_live_failure_and_exact_terminal_replay_do_not_require_released_slot(db):
    case=queued_task(db)
    fence=claim(db,case)[1]
    db.execute('SET LOCAL ROLE photo_worker')
    assert fail(db,case,fence)=='committed'
    assert fail(db,case,fence)=='replayed'
    assert fail(db,case,fence,{'changed':True})=='conflict'
    db.execute('RESET ROLE')
    assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(case['tenant'],)).fetchone()==(1,)
    assert db.execute('SELECT count(*) FROM photo.resource_slots WHERE task_id=%s',(case['task'],)).fetchone()==(0,)


@pytest.mark.parametrize('expiry',['task_lease','slot_lease','deadline'])
def test_final_authority_lock_wait_cannot_cross_failure_lease(database,expiry):
    with psycopg.connect(**database) as seed:
        case=queued_task(seed)
        fence=claim(seed,case)[1]
        seed.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '60 seconds',deadline=clock_timestamp()+interval '60 seconds' WHERE id=%s",(case['task'],))
        seed.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()+interval '60 seconds' WHERE task_id=%s",(case['task'],))
        if expiry=='slot_lease':
            seed.execute("UPDATE photo.resource_slots SET lease_until=clock_timestamp()+interval '2 seconds' WHERE task_id=%s",(case['task'],))
        elif expiry=='deadline':
            seed.execute("UPDATE photo.tasks SET deadline=clock_timestamp()+interval '2 seconds' WHERE id=%s",(case['task'],))
        else:
            seed.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '2 seconds' WHERE id=%s",(case['task'],))
    with psycopg.connect(**database) as blocker, psycopg.connect(**database,autocommit=True) as observer:
        blocker.execute('SELECT worker_id FROM photo.worker_registrations WHERE tenant_id=%s AND worker_id=%s FOR UPDATE',(case['tenant'],case['worker']))
        with psycopg.connect(**database) as worker:
            pid=worker.info.backend_pid
            def finalize():
                worker.execute('SET LOCAL ROLE photo_worker')
                worker.execute("SET LOCAL statement_timeout='8s'")
                return fail(worker,case,fence)
            with ThreadPoolExecutor(max_workers=1) as pool:
                future=pool.submit(finalize)
                deadline=monotonic()+5
                try:
                    while True:
                        waiting=observer.execute("SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=%s",(pid,)).fetchone()
                        if waiting and waiting[0]:break
                        assert not future.done(), 'failure writer did not wait for final authority'
                        assert monotonic()<deadline, 'authority lock wait not reached'
                        sleep(.01)
                    if expiry=='slot_lease':
                        observer.execute("SELECT pg_sleep(greatest(0,extract(epoch FROM lease_until-clock_timestamp()))+.05) FROM photo.resource_slots WHERE task_id=%s",(case['task'],))
                    elif expiry=='deadline':
                        observer.execute("SELECT pg_sleep(greatest(0,extract(epoch FROM deadline-clock_timestamp()))+.05) FROM photo.tasks WHERE id=%s",(case['task'],))
                    else:
                        observer.execute("SELECT pg_sleep(greatest(0,extract(epoch FROM lease_until-clock_timestamp()))+.05) FROM photo.tasks WHERE id=%s",(case['task'],))
                finally:
                    blocker.rollback()
                assert future.result(timeout=5)=='stale'
    with psycopg.connect(**database) as inspect:
        assert_unmodified(inspect,case)


def test_installed_failure_guard_orders_authority_before_clock_before_update(db):
    source,owner,secdef=db.execute("SELECT prosrc,pg_get_userbyid(proowner),prosecdef FROM pg_proc WHERE oid='photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb)'::regprocedure").fetchone()
    assert owner=='photo_fn_owner' and secdef is True
    authority=source.index('-- 02543: final failure authority')
    clock=source.index('-- 02543: no authority/helper wait')
    update=source.index(' UPDATE photo.tasks SET state=CASE WHEN cancelled')
    assert source.index('IF old_error=p_error AND old_metrics=p_metrics')<authority<clock<update
    assert 'FOR SHARE' in source[authority:clock]
    assert 'photo.worker_authorized' in source[authority:clock]
    assert 'slot.lease_until IS NULL' in source[clock:update]
    assert 't.lease_until IS NULL' in source[clock:update]


# Execute only in the later migration-test stage against the specified old head.

@pytest.mark.migration_head(2542)
def test_failure_upgrade_preserves_identity_and_acl(db):
    from pathlib import Path
    query="SELECT oid,proowner,proacl::text,prosecdef,prosrc FROM pg_proc WHERE oid='photo.commit_task_failure(uuid,integer,bigint,uuid,jsonb,jsonb)'::regprocedure"
    before=db.execute(query).fetchone()
    migration=(Path(__file__).resolve().parents[2]/'backend/db/migrations/02543_task_failure_final_live_fence.sql').read_text().split('-- +goose Down')[0]
    db.execute(migration)
    after=db.execute(query).fetchone()
    assert before[:4]==after[:4]
    assert '-- 02543:' not in before[4] and '-- 02543:' in after[4]
    with pytest.raises(Exception,match='TASK_FAILURE_BASE_SOURCE_SHAPE'):
        with db.transaction():
            db.execute(migration)

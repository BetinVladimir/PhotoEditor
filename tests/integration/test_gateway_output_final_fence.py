"""Authored installed SQL and actual row-wait scenarios; not execution evidence."""
import pytest


def test_output_installed_guards_follow_waits(db):
    begin = db.execute("SELECT prosrc FROM pg_proc WHERE oid='photo.begin_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text,text)'::regprocedure").fetchone()[0]
    complete = db.execute("SELECT prosrc FROM pg_proc WHERE oid='photo.complete_output(uuid,integer,bigint,uuid,text,uuid,text,bigint,text,text)'::regprocedure").fetchone()[0]
    assert begin.index('FOR UPDATE;') < begin.index('-- 02536:') < begin.index("old.state='ready'")
    assert begin.index('FOR UPDATE OF x;') < begin.rindex('-- 02536:') < begin.index('UPDATE photo.quota_reservations SET bytes=bytes-p_bytes')
    assert 'q.expires_at<=clock_timestamp()' in begin
    assert complete.index('FOR UPDATE;') < complete.index('-- 02536:') < complete.index("u.state='ready'") < complete.index('INSERT INTO photo.artifacts')


@pytest.mark.parametrize('phase', ['begin_replay', 'complete_publish', 'complete_replay'])
def test_expired_during_output_wait_has_no_replay_or_publish(database, phase):
    from concurrent.futures import ThreadPoolExecutor
    from time import monotonic, sleep
    from uuid import uuid4
    import psycopg
    from test_gateway_outputs import setup, begin, complete
    with psycopg.connect(**database) as seed:
        case = setup(seed)
        key = uuid4()
        seed.execute('SET LOCAL ROLE photo_gateway')
        pending = begin(seed, case, key)
        assert pending[0] == 'created'
        if phase != 'complete_publish':
            assert complete(seed, case, pending[1])[0] == 'committed'
        seed.execute('RESET ROLE')
        before = seed.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['tenant'],)).fetchone()
        seed.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '2 seconds' WHERE id=%s", (case['task'],))
    with psycopg.connect(**database) as blocker, psycopg.connect(**database, autocommit=True) as observer, psycopg.connect(**database) as gateway:
        blocker.execute('SELECT id FROM photo.output_uploads WHERE id=%s FOR UPDATE', (pending[1],))
        pid = gateway.info.backend_pid
        def request():
            gateway.execute('SET LOCAL ROLE photo_gateway')
            gateway.execute("SET LOCAL statement_timeout='8s'")
            return begin(gateway, case, key) if phase == 'begin_replay' else complete(gateway, case, pending[1])
        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(request)
            deadline = monotonic()+5
            try:
                while True:
                    row = observer.execute("SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=%s", (pid,)).fetchone()
                    if row and row[0]: break
                    assert not future.done(), 'output request did not reach receipt wait'
                    assert monotonic()<deadline, 'receipt wait not observed'
                    sleep(.01)
                observer.execute("SELECT pg_sleep(greatest(0,extract(epoch FROM lease_until-clock_timestamp()))+.05) FROM photo.tasks WHERE id=%s", (case['task'],))
            finally:
                blocker.rollback()
            assert future.result(timeout=5)[0] == 'stale'
    with psycopg.connect(**database) as inspect:
        assert inspect.execute('SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (case['tenant'],)).fetchone() == before
        expected = 0 if phase == 'complete_publish' else 1
        assert inspect.execute('SELECT count(*) FROM photo.artifacts WHERE id=%s', (pending[2],)).fetchone() == (expected,)

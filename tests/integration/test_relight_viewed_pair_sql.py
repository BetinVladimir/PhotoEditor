"""Relational pair gate scenarios; no successful full decision is claimed."""
from copy import deepcopy
import time
from concurrent.futures import ThreadPoolExecutor
from uuid import uuid4
import psycopg
import pytest
from relight_viewed_pair_fixture import relight_viewed_pair_fixture, pair_ready


def test_pair_ready_uses_actual_text_receipt_and_locked_lineage(db):
    fixture = relight_viewed_pair_fixture(db)
    assert pair_ready(db, fixture) is True
    for field, value in [('session', str(uuid4())), ('source', str(uuid4())),
                         ('sha256', '0' * 64), ('object_version', 'foreign'),
                         ('object_key_sha256', '0' * 64), ('variant', 'preview2560')]:
        changed = deepcopy(fixture['grant']); changed['after'][field] = value
        assert pair_ready(db, fixture, changed) is False
    for side in ('before', 'after'):
        for key in ('images', 'manifests'):
            with db.transaction(force_rollback=True):
                db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s", (fixture[key][side],))
                assert pair_ready(db, fixture) is False
            assert pair_ready(db, fixture) is True
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (fixture['tasks']['after'],))
        assert pair_ready(db, fixture) is False
    assert pair_ready(db, fixture) is True


def test_pair_expiry_is_checked_after_an_observed_row_lock_wait(database):
    with psycopg.connect(**database) as setup:
        fixture = relight_viewed_pair_fixture(setup)
    with psycopg.connect(**database) as blocker:
        blocker.execute('SELECT id FROM photo.runs WHERE id=%s FOR UPDATE', (fixture['presentation'],))
        expiry = blocker.execute("SELECT floor(extract(epoch FROM clock_timestamp()))::bigint+4").fetchone()[0]
        for pin in [fixture['grant'], fixture['grant']['before'], fixture['grant']['after']]:
            pin['expires_unix'] = expiry
        app = 'relight-viewed-wait-' + uuid4().hex
        def waiting_read():
            with psycopg.connect(**database, application_name=app) as waiting:
                waiting.execute("SET LOCAL statement_timeout='12s'")
                return pair_ready(waiting, fixture)
        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(waiting_read)
            try:
                deadline = time.monotonic() + 5
                while time.monotonic() < deadline:
                    # PostgreSQL caches pg_stat_activity within a transaction;
                    # refresh it before observing the independently running PID.
                    blocker.execute('SELECT pg_stat_clear_snapshot()')
                    blocked = blocker.execute("SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE application_name=%s AND wait_event_type='Lock' AND pg_backend_pid()=ANY(pg_blocking_pids(pid)))", (app,)).fetchone()[0]
                    if blocked:
                        break
                    assert not future.done(), 'pair read did not wait on the controlled row lock'
                    time.sleep(.02)
                else:
                    pytest.fail('pair read never reached an observed PostgreSQL lock wait')
                while blocker.execute('SELECT extract(epoch FROM clock_timestamp())').fetchone()[0] < expiry:
                    time.sleep(.02)
            finally:
                blocker.rollback()
            assert future.result(timeout=12) is False
    with psycopg.connect(**database) as check:
        assert check.execute('SELECT state,revision FROM photo.reviews WHERE id=%s', (fixture['review'],)).fetchone() == ('pending', 1)

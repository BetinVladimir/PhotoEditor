"""Own removal selection row contention in conftest's disposable private database.

Committed setup is allowed only in that isolated database, dropped by its
fixture. This module deliberately does not import the external-DSN fixture.
Single-target coverage is groundwork, not full multi-target lock qualification.
"""
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from time import monotonic

import psycopg
import pytest
from removal_preview_receipt_fixture import receipt_fixture, seal, lock_selection
from test_v27_removal_apply_receipt import _selection_state


def setup_selection(config):
    with psycopg.connect(**config) as db:
        data = receipt_fixture(db)
        receipt = seal(db, data)
        assert lock_selection(db, data, receipt) == [(data[1], receipt)]
        outputs = dict(db.execute('SELECT output_role,id FROM photo.artifacts '
            'WHERE tenant_id=%s AND created_by_task_id=%s', (data[0][0], data[7])).fetchall())
        assert len(outputs) == 8
        return data, receipt, outputs


@pytest.mark.parametrize('locked', [
    'target', 'preview', 'producer', 'result',
    'before', 'after', 'affected', 'protected', 'preview_presentation',
    'preview_evidence', 'working_image', 'pixels',
])
def test_selected_bundle_holds_each_authority_row_until_transaction_end(database, locked):
    data, receipt, outputs = setup_selection(database)
    rows = dict(target=('semantic_targets', data[0][4]), preview=('runs', data[1]),
        producer=('tasks', data[7]), result=('task_results', data[8]))
    rows.update({role: ('artifacts', aid) for role, aid in outputs.items()})
    table, aid = rows[locked]
    # Table identifiers come only from the fixed allowlist above.
    query = 'SELECT id FROM photo.' + table + ' WHERE id=%s FOR UPDATE NOWAIT'
    with psycopg.connect(**database) as holder, psycopg.connect(**database) as contender:
        try:
            # The helper uses a nested savepoint; keep an outer transaction open.
            holder.execute('BEGIN')
            holder.execute("SET LOCAL statement_timeout='10s'")
            assert lock_selection(holder, data, receipt) == [(data[1], receipt)]
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with contender.transaction():
                    contender.execute(query, (aid,)).fetchone()
            holder.rollback()
            with contender.transaction(force_rollback=True):
                assert contender.execute(query, (aid,)).fetchone() == (aid,)
        finally:
            holder.rollback()
            contender.rollback()


@pytest.mark.parametrize('commit_revocation', [False, True])
def test_selection_rechecks_output_after_waiting_for_metadata_writer(database, commit_revocation):
    data, receipt, outputs = setup_selection(database)
    expected = [(data[1], receipt)]
    with psycopg.connect(**database) as writer, psycopg.connect(**database) as reader, \
            psycopg.connect(**database, autocommit=True) as observer:
        writer_pid = writer.info.backend_pid
        reader_pid = reader.info.backend_pid
        writer.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
            (data[0][0], outputs['working_image']))
        started = Event()

        def read():
            reader.execute("SET LOCAL statement_timeout='10s'")
            started.set()
            try:
                return lock_selection(reader, data, receipt), None
            except psycopg.Error as error:
                return None, error.sqlstate
            finally:
                reader.rollback()

        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(read)
            try:
                assert started.wait(2), 'reader did not start'
                deadline = monotonic() + 5
                blocked = False
                while monotonic() < deadline and not future.done():
                    blocked = observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))',
                        (writer_pid, reader_pid)).fetchone()[0]
                    if blocked:
                        break
                    # Bounded observation interval, not a guessed race delay.
                    Event().wait(.01)
                assert blocked, 'selection did not wait for the output metadata writer'
                if commit_revocation:
                    writer.commit()
                else:
                    writer.rollback()
                before = _selection_state(observer, data)
                result, sqlstate = future.result(timeout=12)
                if commit_revocation:
                    assert result is None and sqlstate == '40001'
                else:
                    assert result == expected and sqlstate is None
                assert _selection_state(observer, data) == before
            finally:
                # Release the blocker even if an assertion fails, before join.
                writer.rollback()

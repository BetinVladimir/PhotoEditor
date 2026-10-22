"""Observable tenant-lock rejection/cancellation schedules; metadata SQL only."""
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from time import monotonic
import psycopg
import pytest
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture
from test_v27_semantic_child_adoption_rejection import reject
from test_v27_semantic_child_canonical_completion import finalize


@pytest.mark.parametrize('winner', ['cancel', 'reject'])
def test_child_rejection_and_cancellation_serialize_under_lifecycle_lock(database, winner):
    with psycopg.connect(**database) as seed:
        values, task, commit = successful_copy_metadata_fixture(seed)
        tenant, task_id, _, _, result_id = commit[:5]
        child = values[4]
    with psycopg.connect(**database) as holder, psycopg.connect(**database) as contender, \
            psycopg.connect(**database, autocommit=True) as observer:
        def cancel(db):
            # Same lifecycle -> run order as application mutation paths.
            # Privileged metadata cancellation isolates ordering, not API auth.
            db.execute('SELECT id FROM photo.tenants WHERE id=%s FOR UPDATE', (tenant,))
            db.execute("UPDATE photo.runs SET cancel_requested=true,status='cancelling' "
                       "WHERE tenant_id=%s AND id=%s AND status='running'", (tenant, child))
        holder.execute('BEGIN')
        if winner == 'cancel': cancel(holder)
        else: assert reject(holder, tenant, task_id, result_id) is True
        def second():
            try:
                if winner == 'cancel': outcome = reject(contender, tenant, task_id, result_id)
                else: cancel(contender); outcome = None
                contender.commit()
                return outcome
            except BaseException:
                contender.rollback()
                raise
        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(second)
            try:
                deadline = monotonic() + 5
                blocked = False
                while monotonic() < deadline and not future.done():
                    blocked = observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))',
                        (holder.info.backend_pid, contender.info.backend_pid)).fetchone()[0]
                    if blocked: break
                    Event().wait(.01)
                assert blocked, 'contender never reached observed lifecycle wait'
                holder.commit()
                assert future.result(timeout=10) is (False if winner == 'cancel' else None)
            finally:
                holder.rollback()
                if not future.done(): contender.cancel()
        expected = 'cancelled' if winner == 'cancel' else 'failed'
        assert finalize(holder, tenant, child) == expected
        holder.commit()
        assert observer.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                                (tenant, child)).fetchall() == [(expected,)]
        assert observer.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s "
                                "AND type='v2.adoption.failed'", (tenant, child)).fetchone() == (0 if winner == 'cancel' else 1,)
        assert observer.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type=%s',
                                (tenant, child, 'semantic.child.' + expected)).fetchone() == (1,)

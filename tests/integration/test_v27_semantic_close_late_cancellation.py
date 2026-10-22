"""Cancel a close at its final event cursor wait, then retry the same request.

Private conftest database only; no authority functions or triggers replaced.
"""
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from time import monotonic
import psycopg
from semantic_close_fixture import close_fixture, commit_close, replay_close
from removal_transaction_state import tenant_transaction_state


def test_close_cancelled_at_late_event_wait_rolls_back_all_rows_and_retries(database):
    with psycopg.connect(**database) as seed:
        f=close_fixture(seed)
    tenant=f['args'][0]
    with psycopg.connect(**database) as blocker, psycopg.connect(**database) as reader, \
            psycopg.connect(**database,autocommit=True) as observer:
        before=tenant_transaction_state(observer,tenant)
        # Explicit close decisions already created this cursor. Only lock it.
        assert blocker.execute('SELECT tenant_id FROM photo.tenant_event_cursors WHERE tenant_id=%s FOR UPDATE',
                               (tenant,)).fetchone()==(tenant,)
        started=Event()

        def close():
            started.set()
            try:
                return commit_close(reader,f),None
            except psycopg.Error as error:
                return None,error.sqlstate
            finally:
                reader.rollback()

        with ThreadPoolExecutor(max_workers=1) as pool:
            future=pool.submit(close)
            try:
                assert started.wait(2)
                deadline=monotonic()+5
                blocked=False
                while monotonic()<deadline and not future.done():
                    blocked=observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))',
                        (blocker.info.backend_pid,reader.info.backend_pid)).fetchone()[0]
                    if blocked:break
                    Event().wait(.01)
                assert blocked,'close did not reach final event cursor lock'
                # The close is now past parent/receipt/batch mutations, all private.
                assert tenant_transaction_state(observer,tenant)==before
                reader.cancel()
                assert future.result(timeout=10)==(None,'57014')
                assert tenant_transaction_state(observer,tenant)==before
            finally:
                blocker.rollback()
                if not future.done():reader.cancel()
        # Both cancellation and caller transaction rollback release the fence.
        assert replay_close(reader,f)==('absent',None)
        result=commit_close(reader,f)
        assert replay_close(reader,f)==('replayed',result)
        for table,predicate in (
            ('v2_review_batches',"action='close'"),
            ('run_events',"type='v27.review.closed'"),
            ('outbox',"routing_key='v27.review.closed'"),
            ('idempotency_records',"route='SemanticIntent.CloseReview'"),
        ):
            assert observer.execute('SELECT count(*) FROM photo.'+table+' WHERE tenant_id=%s AND '+predicate,
                                    (tenant,)).fetchone()==(1,)

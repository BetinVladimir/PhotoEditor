"""Deterministic committed SQL lock ordering between phase sync and cancellation."""
from concurrent.futures import ThreadPoolExecutor
import time
import psycopg
import pytest
from test_v26_render_finalizer_tenant_scope import seed_failed_batch


@pytest.mark.parametrize('first',['cancel','sync'])
def test_phase_and_cancel_keep_cancellation_authoritative(database,first):
    with psycopg.connect(**database) as db:
        tenant,batch=seed_failed_batch(db)
        actor,session=db.execute("SELECT created_by,config_snapshot->>'auth_session_id' FROM photo.runs WHERE id=%s",(batch,)).fetchone()
    with psycopg.connect(**database) as writer,psycopg.connect(**database) as other,psycopg.connect(**database,autocommit=True) as observer:
        writer.execute('SET LOCAL ROLE photo_scheduler')
        other.execute('SET LOCAL ROLE photo_scheduler')
        other.execute("SET LOCAL statement_timeout='5s'")
        cancel='SELECT photo.cancel_v26_render_batch(%s,%s,%s,%s)'
        args=(tenant,batch,actor,session)
        if first=='cancel':
            assert writer.execute(cancel,args).fetchone()==(True,)
            # Uncommitted cancellation holds the tenant lock. Sync must skip it.
            assert other.execute('SELECT photo.sync_v26_render_batch_state()').fetchone()==(False,)
            other.commit();writer.commit()
        else:
            assert writer.execute('SELECT photo.sync_v26_render_batch_state()').fetchone()==(True,)
            with ThreadPoolExecutor(max_workers=1) as pool:
                pending=pool.submit(lambda:other.execute(cancel,args).fetchone())
                deadline=time.monotonic()+2
                while time.monotonic()<deadline:
                    wait=observer.execute('SELECT wait_event_type FROM pg_stat_activity WHERE pid=%s',(other.info.backend_pid,)).fetchone()
                    if wait==('Lock',):break
                    time.sleep(.01)
                else:
                    writer.rollback()
                    raise AssertionError('cancellation did not wait on the held scheduler lock')
                writer.commit()
                assert pending.result(timeout=5)==(True,)
            other.commit()
        expected_revision=2 if first=='cancel' else 3
        assert observer.execute('SELECT status,cancel_requested,revision FROM photo.runs WHERE id=%s',(batch,)).fetchone()==('cancelling',True,expected_revision)
        writer.execute('SET LOCAL ROLE photo_scheduler')
        assert writer.execute('SELECT photo.sync_v26_render_batch_state()').fetchone()==(False,)
        assert writer.execute('SELECT photo.finalize_v26_render_batch()').fetchone()==(False,)
        writer.commit()
        assert observer.execute('SELECT status,cancel_requested,revision FROM photo.runs WHERE id=%s',(batch,)).fetchone()==('cancelling',True,expected_revision)
        assert observer.execute("SELECT count(*) FROM photo.run_events WHERE run_id=%s AND type='run.running'",(batch,)).fetchone()==(int(first=='sync'),)

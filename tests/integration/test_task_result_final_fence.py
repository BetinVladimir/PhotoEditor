"""Catalog check for the installed base commit behind all operation wrappers.

Runtime lock-race coverage belongs to deep testing; this authored scenario
ensures the migrated body contains its second fence after output locking.
"""
import pytest

def test_installed_result_base_rechecks_after_output_locks(db):
    rows=db.execute("""SELECT p.prosrc,pg_get_userbyid(p.proowner),p.prosecdef
      FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
      WHERE n.nspname='photo' AND position(
      ' INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest)'
      in p.prosrc)>0""").fetchall()
    assert len(rows)==1
    source,owner,security_definer=rows[0]
    assert owner=='photo_fn_owner' and security_definer is True
    output_lock=source.index('OUTPUT_NOT_VERIFIED')
    late_fence=source.index('-- 02535:')
    result_insert=source.index(' INSERT INTO photo.task_results(')
    assert output_lock<late_fence<result_insert
    guard=source[late_fence:result_insert]
    assert 't.lease_until<=clock_timestamp()' in guard
    assert 't.deadline<=clock_timestamp()' in guard
    assert 'w.db_principal=session_user AND w.enabled' in guard
    assert 'photo.worker_authorized' in guard
    assert "SELECT 'stale'" in guard and "SELECT 'cancelled'" in guard


def test_expiry_while_waiting_for_output_lock_does_not_create_result(database):
    from concurrent.futures import ThreadPoolExecutor
    from time import monotonic, sleep
    from uuid import uuid4
    import psycopg
    from test_task_leases import queued_task, claim, commit, manifest
    with psycopg.connect(**database) as seed:
        case=queued_task(seed)
        claimed=claim(seed,case)
        assert claimed[0]=='claimed'
        fence=claimed[1]
        artifact=uuid4()
        seed.execute("""INSERT INTO photo.artifacts(id,tenant_id,bucket,object_key,sha256,bytes,
          media_type,created_by_task_id,produced_attempt,produced_fence,verified_at)
          VALUES(%s,%s,'test',%s,%s,20,'application/json',%s,1,%s,clock_timestamp())""",
          (artifact,case['tenant'],str(artifact),'f'*64,case['task'],fence))
        seed.execute("UPDATE photo.tasks SET lease_until=clock_timestamp()+interval '2 seconds' WHERE id=%s",(case['task'],))
    body=manifest([dict(artifact_id=str(artifact),sha256='f'*64)])
    with psycopg.connect(**database) as blocker, psycopg.connect(**database,autocommit=True) as observer:
        blocker.execute('SELECT id FROM photo.artifacts WHERE id=%s FOR UPDATE',(artifact,))
        with psycopg.connect(**database) as worker:
            pid=worker.info.backend_pid
            def finalize():
                worker.execute('SET LOCAL ROLE photo_worker')
                worker.execute("SET LOCAL statement_timeout='8s'")
                return commit(worker,case,fence,body)
            with ThreadPoolExecutor(max_workers=1) as pool:
                future=pool.submit(finalize)
                deadline=monotonic()+5
                try:
                    while True:
                        waiting=observer.execute("SELECT wait_event_type='Lock' FROM pg_stat_activity WHERE pid=%s",(pid,)).fetchone()
                        if waiting and waiting[0]:break
                        assert not future.done(), 'commit did not wait for output lock'
                        assert monotonic()<deadline, 'output lock wait was not reached'
                        sleep(.01)
                    observer.execute("SELECT pg_sleep(greatest(0,extract(epoch FROM lease_until-clock_timestamp()))+.05) FROM photo.tasks WHERE id=%s",(case['task'],))
                finally:
                    blocker.rollback()
                assert future.result(timeout=5)[0]=='stale'
    with psycopg.connect(**database) as inspect:
        assert inspect.execute('SELECT count(*) FROM photo.task_results WHERE task_id=%s',(case['task'],)).fetchone()==(0,)
        assert inspect.execute('SELECT canonical_result_id FROM photo.tasks WHERE id=%s',(case['task'],)).fetchone()==(None,)


@pytest.mark.migration_head(2534)
def test_02535_upgrade_preserves_function_identity_acl_and_wrappers(db):
    from pathlib import Path
    import pytest
    root=Path(__file__).resolve().parents[2]
    query="""SELECT p.oid,p.proname,p.proowner,p.proacl::text,p.prosecdef,p.prosrc
      FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
      WHERE n.nspname='photo' AND p.proname LIKE 'commit_task_success%%' ORDER BY p.oid"""
    before=db.execute(query).fetchall()
    assert before
    migration=(root/'backend/db/migrations/02535_task_result_after_output_lock_fence.sql').read_text().split('-- +goose Down')[0]
    db.execute(migration)
    after=db.execute(query).fetchall()
    assert [row[:5] for row in after]==[row[:5] for row in before]
    changed=[(a,b) for a,b in zip(before,after) if a[5]!=b[5]]
    assert len(changed)==1
    assert '-- 02535:' not in changed[0][0][5]
    assert '-- 02535:' in changed[0][1][5]
    with pytest.raises(Exception,match='TASK_RESULT_BASE_SOURCE_SHAPE'):
        with db.transaction():
            db.execute(migration)

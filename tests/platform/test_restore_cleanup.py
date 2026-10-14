from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_cleanup import fence_restored_cleanup


@pytest.mark.parametrize('mode',['valid','foreign','unprivileged','remaining','bad_receipt','cancelled'])
def test_cleanup_fencing_preserves_metadata_and_is_atomic(mode):
    events=[];claims=(('output_uploads','cleanup_lease_until'),('backend_documents','cleanup_lease_until'),('ingest_originals','cleanup_lease_until'),('deletion_jobs','cleanup_lease_until'),('staging_cleanup','lease_until'),('upload_abort_cleanup','lease_until'),('artifacts','gc_lease_until'),('v30_archive_uploads','cleanup_lease_until'))
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            except BaseException:events.append('rollback');raise
            else:events.append('commit')
        def execute(self,sql):
            events.append(sql);row=None;count=0
            if sql.startswith('SELECT current_database'):
                row=('foreign' if mode=='foreign' else 'isolated',mode!='unprivileged')
            elif sql.startswith('UPDATE'):
                assert any(sql==f"UPDATE photo.{table} SET {column}='-infinity'::timestamptz WHERE {column} IS NOT NULL AND {column}>'-infinity'::timestamptz" for table,column in claims)
                assert 'cleanup_token=' not in sql and 'status=' not in sql and 'state=' not in sql
                count=-1 if mode=='bad_receipt' else 2
            elif sql.startswith('SELECT count'):row=(1 if mode=='remaining' else 0,)
            return SimpleNamespace(fetchone=lambda:row,rowcount=count)
    def active():
        if mode=='cancelled' and any(e.startswith('UPDATE') for e in events):
            raise RuntimeError('cancelled')
    def run():return fence_restored_cleanup(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=active)
    if mode=='valid':
        result=run();assert result.expired_claims==tuple((table,2) for table,column in claims)
        assert events.count('begin')==1 and 'commit' in events
        assert len([e for e in events if e.startswith('UPDATE')])==8
        assert len([e for e in events if e.startswith('SELECT count')])==8
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'rollback' in events and 'commit' not in events
        if mode in ('foreign','unprivileged'):assert not any(e.startswith('UPDATE') for e in events)


@pytest.mark.parametrize('failed_table',['staging_cleanup','upload_abort_cleanup','artifacts','v30_archive_uploads'])
def test_late_queue_failure_rolls_back_all_preceding_fences(failed_table):
    committed=[];pending=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            try:yield
            except BaseException:pending.clear();raise
            else:committed.extend(pending)
        def execute(self,sql):
            if sql.startswith('SELECT current_database'):
                return SimpleNamespace(fetchone=lambda:('isolated',True))
            if sql.startswith('UPDATE'):
                table=sql.split()[1].split('.')[1]
                if table==failed_table:raise RuntimeError('storage/schema failure')
                pending.append(table)
                return SimpleNamespace(rowcount=1)
            return SimpleNamespace(fetchone=lambda:(0,))
    with pytest.raises(RuntimeError):
        fence_restored_cleanup(Connection(),expected_database='isolated',
            assert_isolated_and_paused=lambda:None,check_active=lambda:None)
    assert committed==[] and pending==[]

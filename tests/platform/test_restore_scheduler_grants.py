from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_scheduler_grants import fence_restored_scheduler_grants


@pytest.mark.parametrize('mode',['valid','foreign','unprivileged','remaining','second_update_failure','archive_update_failure','cancelled'])
def test_scheduler_fencing_preserves_completion_tokens_and_graph_state_atomically(mode):
    events=[];pending=[];committed=[]
    claims=(('upload_sessions','completion_lease_until'),('graph_expansions','expansion_lease_until'),('v30_archive_uploads','completion_lease_until'))
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            try:yield
            except BaseException:pending.clear();events.append('rollback');raise
            else:committed.extend(pending);events.append('commit')
        def execute(self,sql):
            events.append(sql);row=None;count=0
            if sql.startswith('SELECT current_database'):
                row=('foreign' if mode=='foreign' else 'isolated',mode!='unprivileged')
            elif sql.startswith('UPDATE'):
                assert any(sql==f"UPDATE photo.{table} SET {column}='-infinity'::timestamptz WHERE {column} IS NOT NULL AND {column}>'-infinity'::timestamptz" for table,column in claims)
                if mode=='second_update_failure' and 'graph_expansions' in sql:raise RuntimeError('database failure')
                if mode=='archive_update_failure' and 'v30_archive_uploads' in sql:raise RuntimeError('archive schema failure')
                pending.append(sql);count=2
            elif sql.startswith('SELECT count'):row=(1 if mode=='remaining' else 0,)
            return SimpleNamespace(fetchone=lambda:row,rowcount=count)
    def active():
        if mode=='cancelled' and pending:raise RuntimeError('cancelled')
    def run():return fence_restored_scheduler_grants(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=active)
    if mode=='valid':
        result=run();assert result.expired_claims==tuple((table,2) for table,column in claims)
        assert len(committed)==3 and 'commit' in events
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert committed==[] and pending==[] and 'rollback' in events
        if mode in ('foreign','unprivileged'):assert not any(e.startswith('UPDATE') for e in events)

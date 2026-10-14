from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_leases import fence_restored_compute_leases


@pytest.mark.parametrize('mode',['valid','foreign','unprivileged','limit','remaining','bad_receipt','paused_failure'])
def test_restore_fencing_uses_short_canonical_reaper_transactions(mode):
    events=[];transactions=0;reaps=iter([1,1,0]);counts=iter([2,1 if mode=='remaining' else 0])
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            nonlocal transactions
            transactions+=1;events.append('begin')
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            events.append(sql)
            row=None;rowcount=0
            if sql.startswith('SELECT current_database'):row=('foreign' if mode=='foreign' else 'isolated',mode!='unprivileged')
            elif sql.startswith('SELECT count'):row=(next(counts),)
            elif sql.startswith('UPDATE photo.tasks'):rowcount=1
            elif sql.startswith('UPDATE photo.resource'):rowcount=2
            elif sql=='SELECT photo.reap_tasks(1)':row=(2 if mode=='bad_receipt' else next(reaps),)
            return SimpleNamespace(fetchone=lambda:row,rowcount=rowcount)
    def paused():
        if mode=='paused_failure':raise RuntimeError('writers active')
    def run():return fence_restored_compute_leases(Connection(),expected_database='isolated',
        assert_isolated_and_paused=paused,check_active=lambda:None,
        maximum_tasks=1 if mode=='limit' else 10)
    if mode=='valid':
        result=run()
        assert result.recovered_tasks==2 and result.expired_running==1
        assert result.expired_reservations==2 and transactions==5
        assert events.count('SELECT photo.reap_tasks(1)')==3
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('foreign','unprivileged','limit','paused_failure'):
            assert not any(event.startswith('UPDATE') for event in events)

from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_mail import recover_restored_mail


@pytest.mark.parametrize('mode',['valid','foreign','unprivileged','limit','remaining','bad_receipt','cancelled'])
def test_mail_recovery_expires_then_uses_canonical_reconciler(mode):
    events=[];counts=iter([3,0]);reconciled=iter([3,0])
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
            elif sql.startswith('SELECT count'):
                row=(next(counts),)
                if 'WHERE status=' in sql and mode=='remaining':row=(1,)
            elif sql.startswith('UPDATE'):
                assert sql=="UPDATE photo.mail_payloads SET lease_until='-infinity'::timestamptz WHERE status='sending'"
                count=2
            elif sql=='SELECT photo.reconcile_mail(100)':
                row=(101 if mode=='bad_receipt' else next(reconciled),)
            return SimpleNamespace(fetchone=lambda:row,rowcount=count)
    def active():
        if mode=='cancelled' and any(e.startswith('UPDATE') for e in events):
            raise RuntimeError('cancelled')
    def run():return recover_restored_mail(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=active,
        maximum_payloads=1 if mode=='limit' else 10)
    if mode=='valid':
        result=run();assert result.sending_leases_expired==2 and result.reconciled_payloads==3
        assert events.count('commit')==4
        assert events.index("UPDATE photo.mail_payloads SET lease_until='-infinity'::timestamptz WHERE status='sending'")<events.index('SELECT photo.reconcile_mail(100)')
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'rollback' in events
        if mode in ('foreign','unprivileged','limit'):assert not any(e.startswith('UPDATE') for e in events)

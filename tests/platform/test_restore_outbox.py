from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from uuid import UUID
from photo_worker.runtime.restore_outbox import prepare_restore_outbox


@pytest.mark.parametrize('mode',['valid','foreign','unprivileged','remaining_task','remaining_mail','bad_receipt','cancelled'])
def test_outbox_preparation_only_changes_claims_in_one_transaction(mode):
    events=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            except BaseException:events.append('rollback');raise
            else:events.append('commit')
        @contextmanager
        def cursor(self,**kwargs):
            class Cursor:
                def execute(self,sql):
                    assert 'ORDER BY id' in sql
                def __iter__(self):
                    return iter((UUID(int=i),UUID(int=100),UUID(int=200+i),'tasks','route',{'task':i}) for i in range(1,6))
            yield Cursor()
        def execute(self,sql):
            events.append(sql);row=None;count=0
            if sql.startswith('SELECT current_database'):
                row=('foreign' if mode=='foreign' else 'isolated',mode!='unprivileged')
            elif sql.startswith('UPDATE'):
                assert sql in (
                    'UPDATE photo.outbox SET publish_lease_until=NULL WHERE publish_lease_until IS NOT NULL',
                    'UPDATE photo.auth_mail_outbox SET publish_lease_until=NULL WHERE publish_lease_until IS NOT NULL')
                count=-1 if mode=='bad_receipt' else 2
            elif sql.startswith('SELECT\n'):
                row=(1 if mode=='remaining_task' else 0,1 if mode=='remaining_mail' else 0,5,3)
            return SimpleNamespace(fetchone=lambda:row,rowcount=count)
    def active():
        if mode=='cancelled' and any(e.startswith('UPDATE') for e in events):
            raise RuntimeError('cancelled')
    def run():return prepare_restore_outbox(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=active)
    if mode=='valid':
        result=run();assert result.pending_task_events==5 and result.pending_mail_events==3
        assert len(result.pending_task_sha256)==64
        assert result.task_claims_invalidated==2 and result.mail_claims_invalidated==2
        assert events.count('begin')==1 and 'commit' in events
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'rollback' in events and 'commit' not in events
        if mode in ('foreign','unprivileged'):assert not any(e.startswith('UPDATE') for e in events)

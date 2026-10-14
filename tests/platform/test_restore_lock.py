from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_lock import exclusive_restore


@pytest.mark.parametrize('mode',['valid','busy','nested','foreign','body_failure','session_changed','unlock_failure','cancel_after_acquire','lock_lost_during_body'])
def test_restore_session_lock_refuses_concurrency_and_releases_on_failure(mode):
    events=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):yield
        def close(self):events.append('closed')
        def execute(self,sql,params=None):
            events.append(sql)
            if 'pg_try_advisory_lock' in sql:row=(mode!='busy',)
            elif 'pg_advisory_unlock' in sql:row=(mode!='unlock_failure',)
            elif 'pg_backend_pid(),EXISTS' in sql:
                row=('isolated',456 if mode=='session_changed' else 123,
                    not (mode=='lock_lost_during_body' and 'body' in events))
            elif 'SELECT EXISTS' in sql:row=(mode=='nested',)
            elif 'rolsuper' in sql:row=('foreign' if mode=='foreign' else 'isolated',123,True)
            else:row=('isolated',456 if mode=='session_changed' else 123)
            return SimpleNamespace(fetchone=lambda:row)
    def active():
        if mode=='cancel_after_acquire' and any('pg_try_advisory_lock' in e for e in events):
            raise RuntimeError('cancelled')
    def run():
        with exclusive_restore(Connection(),expected_database='isolated',
            assert_isolated_and_paused=lambda:None,check_active=active) as owned:
            events.append('body')
            owned()
            if mode=='body_failure':raise RuntimeError('failed recovery')
    if mode=='valid':run()
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    if mode in ('busy','nested','foreign'):assert 'body' not in events
    if mode in ('valid','body_failure','cancel_after_acquire','lock_lost_during_body'):
        assert any('pg_advisory_unlock' in e for e in events) and 'closed' not in events
    if mode in ('session_changed','unlock_failure'):assert 'closed' in events


def test_owned_guard_preserves_existing_stage_transaction_and_timeout():
    info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
    events=[]
    class Connection:
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            finally:events.append('end')
        def close(self):events.append('closed')
        def execute(self,sql,params=None):
            events.append(sql)
            if 'pg_try_advisory_lock' in sql or 'pg_advisory_unlock' in sql:row=(True,)
            elif 'pg_backend_pid(),EXISTS' in sql or 'rolsuper' in sql:row=('isolated',123,True)
            elif sql.startswith('SELECT EXISTS'):row=(False,)
            else:row=('isolated',123)
            return SimpleNamespace(fetchone=lambda:row)
    connection=Connection();connection.info=info
    with exclusive_restore(connection,expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=lambda:None) as owned:
        info.transaction_status=psycopg.pq.TransactionStatus.INTRANS
        start=len(events);owned();inside=events[start:]
        assert len(inside)==1 and 'pg_backend_pid(),EXISTS' in inside[0]
        info.transaction_status=psycopg.pq.TransactionStatus.IDLE
    assert 'closed' not in events


@pytest.mark.parametrize('denial', ['activity','isolation'])
def test_restore_lock_rejects_boolean_denial_before_sql(denial):
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        def execute(self,*args,**kwargs):pytest.fail('SQL reached after denial')
    with pytest.raises(RuntimeError,match='RESTORE_LOCK_INACTIVE|RESTORE_WRITERS_NOT_PAUSED'):
        with exclusive_restore(Connection(),expected_database='isolated',
            assert_isolated_and_paused=lambda:False if denial=='isolation' else None,
            check_active=lambda:False if denial=='activity' else None):
            pytest.fail('denied lock scope entered')

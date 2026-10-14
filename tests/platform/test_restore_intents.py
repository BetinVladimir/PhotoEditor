from contextlib import contextmanager
from datetime import datetime,timezone
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_intents import invalidate_expired_restore_intents


@pytest.mark.parametrize('mode',['valid','foreign','unprivileged','remaining','bad_receipt','cancelled'])
def test_expired_intents_are_consumed_at_database_cutoff_atomically(mode):
    cutoff=datetime.now(timezone.utc);events=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            except BaseException:events.append('rollback');raise
            else:events.append('commit')
        def execute(self,sql,params=None):
            events.append(sql);row=None;count=0
            if sql.startswith('SELECT current_database'):
                row=('foreign' if mode=='foreign' else 'isolated',cutoff,mode!='unprivileged')
            elif sql.startswith('UPDATE'):
                assert params==(cutoff,cutoff)
                assert 'consumed_at IS NULL AND expires_at<=' in sql
                count=-1 if mode=='bad_receipt' else 4
            elif sql.startswith('SELECT count'):
                assert params==(cutoff,);row=(1 if mode=='remaining' else 0,)
            return SimpleNamespace(fetchone=lambda:row,rowcount=count)
    def active():
        if mode=='cancelled' and any(e.startswith('UPDATE') for e in events):
            raise RuntimeError('cancelled')
    def run():return invalidate_expired_restore_intents(Connection(),expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=active)
    if mode=='valid':
        result=run();assert result.expired_download_intents==4
        assert result.recovery_time==cutoff and 'commit' in events
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'commit' not in events and 'rollback' in events
        if mode in ('foreign','unprivileged'):assert not any(e.startswith('UPDATE') for e in events)

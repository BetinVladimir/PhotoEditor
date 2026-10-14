from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_purge_probe import verify_purged_tenant_metadata


@pytest.mark.parametrize('mode',['valid','foreign_database','unprivileged','live_tenant','unfinished_job','membership_live','audit_remaining','metadata_remaining','missing_table','catalogue_overflow','cancelled'])
def test_purge_probe_independently_observes_current_metadata(mode):
    queries=[];events=[];tenant=uuid4();job=uuid4()
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            except BaseException:events.append('rollback');raise
            else:events.append('commit')
        def execute(self,statement,params=None):
            text=statement if isinstance(statement,str) else statement.as_string()
            queries.append(text);row=None;rows=[]
            if text.startswith('SELECT current_database'):
                row=('wrong' if mode=='foreign_database' else 'isolated',mode!='unprivileged')
            elif text.startswith('SELECT\n'):
                row=('active' if mode=='live_tenant' else 'deleted',mode!='unfinished_job',mode=='membership_live',mode=='audit_remaining')
            elif text.startswith('SELECT c.relname'):
                names=['artifacts','tasks','outbox','consent_grants','memberships','audit_log','deletion_jobs','new_tenant_table']
                if mode=='missing_table':names.remove('tasks')
                if mode=='catalogue_overflow':names += ['extra_'+str(i) for i in range(513)]
                rows=[(name,) for name in sorted(names)]
            elif text.startswith('SELECT EXISTS'):
                assert params==(tenant,)
                row=(mode=='metadata_remaining' and 'new_tenant_table' in text,)
            return SimpleNamespace(fetchone=lambda:row,fetchall=lambda:rows)
    def active():
        if mode=='cancelled' and any(q.startswith('SELECT EXISTS') for q in queries):raise RuntimeError('cancelled')
    def fixture(scope,deletion):assert (scope,deletion)==(tenant,job)
    def run():return verify_purged_tenant_metadata(Connection(),tenant,job,
        expected_database='isolated',assert_fixture_scope=fixture,check_active=active)
    if mode=='valid':
        result=run();assert 'new_tenant_table' in result.checked_tables
        assert 'memberships' not in result.checked_tables and 'commit' in events
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'rollback' in events and 'commit' not in events
    assert not any(q.startswith(('UPDATE','DELETE','INSERT')) for q in queries)


@pytest.mark.parametrize('denial',['activity','fixture'])
def test_purge_metadata_explicit_false_refuses_before_sql(denial):
    class NoSQL:
        def __getattribute__(self,name):raise AssertionError('SQL reached')
    with pytest.raises(RuntimeError):
        verify_purged_tenant_metadata(NoSQL(),uuid4(),uuid4(),expected_database='isolated',
            assert_fixture_scope=lambda *_:False if denial=='fixture' else None,
            check_active=lambda:False if denial=='activity' else None)

from contextlib import contextmanager
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_isolation import SQLRestoreIsolationGuard


@pytest.mark.parametrize('mode',['valid','wrong_database','wrong_system','wrong_directory',
    'standby','connections_enabled','other_client','unprivileged','external_failure'])
def test_sql_restore_isolation_requires_actual_database_fence(mode):
    events=[];values=['isolated','123456','/srv/restore/pgdata',False,False,False,True]
    replacements={'wrong_database':(0,'production'),'wrong_system':(1,'999'),
        'wrong_directory':(2,'/srv/production/pgdata'),'standby':(3,True),
        'connections_enabled':(4,True),'other_client':(5,True),'unprivileged':(6,False)}
    if mode in replacements:
        index,value=replacements[mode];values[index]=value
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            events.append(sql);return SimpleNamespace(fetchone=lambda:tuple(values))
    def external():
        if mode=='external_failure':raise RuntimeError('outgoing enabled')
    guard=SQLRestoreIsolationGuard(Connection(),expected_database='isolated',
        expected_system_identifier='123456',expected_data_directory='/srv/restore/pgdata',
        assert_external_isolation=external,check_active=lambda:None)
    if mode=='valid':guard()
    else:
        with pytest.raises((ValueError,RuntimeError)):guard()
    assert not any(sql.startswith(('ALTER','UPDATE','DELETE')) for sql in events)
    if mode=='external_failure':assert events==[]


@pytest.mark.parametrize('denial', ['activity','external'])
def test_restore_isolation_rejects_boolean_denial_before_sql(denial):
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        def execute(self,*args,**kwargs):pytest.fail('SQL reached after denial')
    guard=SQLRestoreIsolationGuard(Connection(),expected_database='isolated',
        expected_system_identifier='123456',expected_data_directory='/srv/restore/pgdata',
        assert_external_isolation=lambda:False if denial=='external' else None,
        check_active=lambda:False if denial=='activity' else None)
    with pytest.raises(RuntimeError,match='RESTORE_ISOLATION_INACTIVE|RESTORE_EXTERNAL_ISOLATION_DENIED'):
        guard()

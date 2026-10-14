from contextlib import contextmanager
from datetime import datetime,timezone
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_deletions import DeletionHistoryCheckpoint
from photo_worker.runtime.restore_deletion_catalogue import verify_restore_database_deletions


class Connection:
    def __init__(self,tenant,status):
        self.tenant=tenant;self.status=status;self.database='isolated';self.privileged=True
        self.info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        self.calls=[];self.failed=False
    @contextmanager
    def transaction(self):
        self.calls.append(('begin',None))
        try:yield
        except BaseException:self.failed=True;raise
    def execute(self,sql,args=None):
        self.calls.append((sql,args))
        if sql.startswith('SELECT status'):
            assert args==(self.tenant,)
            result=None if self.status is None else (self.status,)
        else:result=(self.database,'10:20:',datetime.now(timezone.utc),self.privileged)
        return SimpleNamespace(fetchone=lambda:result)


def fixture():
    tenant=uuid4()
    entry={'schema_version':'tenant-deletion-history/v1','sequence':1,'previous_sha256':'0'*64,
           'tenant_id':str(tenant),'phase':'completed','occurred_at':'2026-10-11T12:00:00Z'}
    checkpoint=DeletionHistoryCheckpoint(1,sha256(canonical_bytes(entry)).hexdigest())
    return tenant,entry,checkpoint


@pytest.mark.parametrize('status',[None,'deleting','deleted'])
def test_restore_sql_deletion_reconciliation_binds_tenant_and_snapshot(status):
    tenant,entry,checkpoint=fixture();connection=Connection(tenant,status)
    evidence=verify_restore_database_deletions(connection,[entry],checkpoint,
        expected_database='isolated',check_active=lambda:None)
    assert evidence.database=='isolated' and evidence.history.sequence==1
    assert connection.calls[1][0]=='SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY'
    assert connection.calls[2][0]=='SET LOCAL row_security = off'
    assert len([sql for sql,_ in connection.calls if sql.startswith('SELECT status')])==1


@pytest.mark.parametrize('mode',['active','wrong_database','rls','nested','missing_history','inactive'])
def test_restore_sql_deletion_reconciliation_refuses_unsafe_restore(mode):
    tenant,entry,checkpoint=fixture();connection=Connection(tenant,'deleted');entries=[entry]
    if mode=='active':connection.status='active'
    if mode=='wrong_database':connection.database='production'
    if mode=='rls':connection.privileged=False
    if mode=='nested':connection.info.transaction_status=psycopg.pq.TransactionStatus.INTRANS
    if mode=='missing_history':entries=[]
    with pytest.raises((ValueError,RuntimeError)):
        verify_restore_database_deletions(connection,entries,checkpoint,
            expected_database='isolated',check_active=lambda:False if mode=='inactive' else None)
    if mode in ('nested','inactive'):assert connection.calls==[]
    else:assert connection.failed

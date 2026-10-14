from contextlib import contextmanager
from datetime import datetime, timezone
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace
import psycopg
import pytest
from photo_worker.runtime.restore_catalogue import verify_restore_database_objects, READY_OBJECTS_SQL


class Cursor:
    def __init__(self,rows):self.rows=rows;self.closed=False;self.sql=None
    def __enter__(self):return self
    def __exit__(self,*_):self.closed=True
    def execute(self,sql):self.sql=sql
    def __iter__(self):return iter(self.rows)


class Connection:
    def __init__(self):
        self.info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        self.calls=[];self.finished=False;self.failed=False;self.privileged=True
        self.stream=Cursor([('photos','tenant/artifact','version',sha256(b'image').hexdigest(),5)])
    @contextmanager
    def transaction(self):
        self.calls.append('begin')
        try:yield
        except BaseException:self.failed=True;raise
        finally:self.finished=True
    def execute(self,sql):
        self.calls.append(sql)
        return SimpleNamespace(fetchone=lambda:('isolated_restore','10:20:',datetime.now(timezone.utc),self.privileged))
    def cursor(self,*,name):
        assert name.startswith('restore_objects_')
        return self.stream


def test_restore_catalogue_uses_whole_read_only_snapshot_and_streams():
    connection=Connection();seen=[]
    def source(ref):seen.append(ref);return BytesIO(b'image')
    evidence=verify_restore_database_objects(connection,source,check_active=lambda:None)
    assert evidence.database=='isolated_restore' and evidence.inventory.objects==1
    assert connection.calls[1:3]==['SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY','SET LOCAL row_security = off']
    assert connection.stream.sql==READY_OBJECTS_SQL and connection.stream.itersize==512
    assert connection.stream.closed and connection.finished and not connection.failed
    assert seen[0].version=='version'


@pytest.mark.parametrize('mode',['active_transaction','rls_role','invalid_metadata','conflicting_version','missing_object'])
def test_restore_catalogue_refuses_partial_or_filtered_evidence(mode):
    connection=Connection()
    if mode=='active_transaction':connection.info.transaction_status=psycopg.pq.TransactionStatus.INTRANS
    if mode=='rls_role':connection.privileged=False
    if mode=='invalid_metadata':connection.stream.rows=[('photos','key',None,'a'*64,5)]
    if mode=='conflicting_version':connection.stream.rows*=2
    def source(_):
        if mode=='missing_object':raise FileNotFoundError('missing')
        return BytesIO(b'image')
    with pytest.raises((ValueError,FileNotFoundError)):
        verify_restore_database_objects(connection,source,check_active=lambda:None)
    if mode=='active_transaction':assert connection.calls==[]
    else:assert connection.failed and connection.finished


def test_restore_storage_verifies_bytes_and_quarantine_under_one_snapshot():
    from photo_worker.runtime.restore_catalogue import verify_restore_database_storage
    from photo_worker.runtime.restore_quarantine import RestorePhysicalVersion
    connection=Connection();cursors=[]
    def cursor(*,name):
        current=Cursor(connection.stream.rows);cursors.append(current);return current
    connection.cursor=cursor
    staged=[]
    result=verify_restore_database_storage(connection,lambda _:BytesIO(b'image'),
        [RestorePhysicalVersion('photos','extra','v',4),RestorePhysicalVersion('photos','tenant/artifact','version',5)],
        staged.append,check_active=lambda:None)
    assert connection.calls.count('begin')==1
    assert len(cursors)==2 and all(c.closed for c in cursors)
    assert result.database.inventory.objects==result.quarantine.referenced==1
    assert result.quarantine.extra==1 and staged[0].key=='extra'


def test_restore_storage_missing_physical_reference_fails_entire_stage():
    from photo_worker.runtime.restore_catalogue import verify_restore_database_storage
    connection=Connection()
    def cursor(*,name):return Cursor(connection.stream.rows)
    connection.cursor=cursor
    with pytest.raises(ValueError,match='REFERENCED_VERSION_MISSING'):
        verify_restore_database_storage(connection,lambda _:BytesIO(b'image'),[],lambda _:None,check_active=lambda:None)
    assert connection.failed and connection.finished


@pytest.mark.parametrize('denial_at',[1,2])
def test_restore_database_catalogue_rejects_explicit_false_before_evidence(denial_at):
    connection=Connection();checks=0
    def active():
        nonlocal checks
        checks+=1
        return False if checks==denial_at else None
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        verify_restore_database_objects(connection,lambda _:BytesIO(b'image'),check_active=active)
    if denial_at==1:assert connection.calls==[]
    else:assert connection.finished and connection.failed and connection.stream.sql is None

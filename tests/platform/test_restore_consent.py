from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_consent import RestoreConsentCheckpoint,verify_restore_consents


def catalogue():
    return {'tenant_id':str(uuid4()),'project_id':str(uuid4()),'id':str(uuid4()),
        'purpose':'learning_photographer','person_id':None,'status':'revoked',
        'epoch':3,'revision':4,'policy_version':'v2'}


def checkpoint(rows):
    h=sha256(b'photoeditor.restore-consent-catalogue/v1\n')
    for row in rows:h.update(canonical_bytes(row)+b'\n')
    return RestoreConsentCheckpoint(len(rows),h.hexdigest())


@pytest.mark.parametrize('mode',['valid','resurrected','old_epoch','old_policy','missing','extra','bad_head','duplicate'])
def test_consent_restore_requires_complete_current_authority(mode):
    row=catalogue();current=[row];restored=[dict(row)];pin=checkpoint(current)
    if mode=='resurrected':restored[0]['status']='granted'
    if mode=='old_epoch':restored[0]['epoch']=2
    if mode=='old_policy':restored[0]['policy_version']='v1'
    if mode=='missing':restored=[]
    if mode=='extra':restored.append(catalogue())
    if mode=='bad_head':pin=RestoreConsentCheckpoint(1,'0'*64)
    if mode=='duplicate':current.append(dict(row));restored.append(dict(row));pin=checkpoint(current)
    def run():return verify_restore_consents(iter(current),iter(restored),pin,check_active=lambda:None)
    if mode=='valid':assert run().sha256==pin.sha256
    else:
        with pytest.raises(ValueError):run()


def test_consent_current_bytes_owned_before_restored_io():
    row=catalogue();pin=checkpoint([row])
    def restored():
        row['status']='granted'
        yield dict(row)
    with pytest.raises(ValueError):
        verify_restore_consents(iter([row]),restored(),pin,check_active=lambda:None)


def test_consent_explicit_false_refuses_before_consuming_current_rows():
    row=catalogue();seen=[]
    def current():
        seen.append('read');yield row
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        verify_restore_consents(current(),iter([row]),checkpoint([row]),check_active=lambda:False)
    assert seen==[]


@pytest.mark.parametrize('mode',['valid','foreign','filtered_role','old_epoch'])
def test_database_consent_enumeration_is_privileged_and_snapshot_bound(mode):
    from contextlib import contextmanager
    from types import SimpleNamespace
    import psycopg
    from photo_worker.runtime.restore_consent import verify_restore_database_consents
    row=catalogue();pin=checkpoint([row]);events=[]
    class Cursor:
        itersize=0
        def __enter__(self):events.append('cursor');return self
        def __exit__(self,*args):events.append('closed')
        def execute(self,sql):
            assert sql.endswith('ORDER BY tenant_id,id')
            assert self.itersize==512
        def __iter__(self):
            yield (row['tenant_id'],row['project_id'],row['id'],row['purpose'],None,
                row['status'],2 if mode=='old_epoch' else row['epoch'],row['revision'],row['policy_version'])
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            events.append('begin')
            try:yield
            finally:events.append('end')
        def execute(self,sql):
            events.append(sql)
            return SimpleNamespace(fetchone=lambda:('foreign' if mode=='foreign' else 'isolated',mode!='filtered_role'))
        def cursor(self,*,name):
            assert name=='restore_consent_catalogue'
            return Cursor()
    def run():return verify_restore_database_consents(Connection(),iter([row]),pin,
        expected_database='isolated',check_active=lambda:None)
    if mode=='valid':
        assert run().sha256==pin.sha256
        assert events.index('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')<events.index('cursor')
        assert events[-2:]==['closed','end']
    else:
        with pytest.raises(ValueError):run()
        if mode in ('foreign','filtered_role'):assert 'cursor' not in events
        else:assert 'closed' in events

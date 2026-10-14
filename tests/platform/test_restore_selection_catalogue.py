from contextlib import contextmanager
from datetime import datetime,timedelta,timezone
from types import SimpleNamespace as NS
from hashlib import sha256
from uuid import uuid4
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_selection_catalogue as m
from photo_worker.runtime.restore_inventory import RestoreObject


@pytest.mark.parametrize('mode',['valid','authority','receipt','response','snapshot','payload','expiry','scope','inactive'])
def test_selection_receipt_exact_wire_and_adopted_digest(mode):
    actor=uuid4();applied=m.RestoreLegacyApplyResult(NS(output_collection_id=uuid4(),run_id=uuid4()),uuid4(),uuid4(),uuid4(),
        RestoreObject('photo','tenant/master','v1','a'*64,512))
    target=m.RestoreSelectionTarget('https://restore.test',uuid4(),applied.registration.output_collection_id,
        uuid4() if mode=='scope' else applied.item_id,7,'fresh','Bearer secret')
    payload={'position':1,'captured_at':None,'camera_hash':None}
    digest=m.one_item_selection_digest(applied,payload);snapshot=uuid4();expires=datetime.now(timezone.utc)+timedelta(hours=1)
    raw=canonical_bytes({'id':str(snapshot),'digest':digest,'item_count':1,'expires_at':expires.isoformat()})
    fingerprint=sha256(m.selection_request_body(target)).hexdigest()
    ticket=m.RestoreSelectionTicket(snapshot,digest,expires,fingerprint,'b'*64 if mode=='response' else sha256(raw).hexdigest())
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT request_hash'):
                assert args==(target.tenant_id,actor,'fresh')
                row=(fingerprint,200 if mode=='receipt' else 201,raw,snapshot)
            elif sql.startswith('SELECT i.payload'):
                assert 'c.revision=s.source_revision' in sql and 'ci.asset_version_id=i.asset_version_id' in sql
                assert args[-4:]==(applied.item_id,applied.asset_version_id,applied.artifact_id,applied.object.sha256)
                row=None if mode=='snapshot' else ({**payload,'position':2} if mode=='payload' else payload,
                    expires+timedelta(seconds=1) if mode=='expiry' else expires)
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.observe_fixture_selection(Connection(),target,ticket,actor,applied,
        expected_database='restore',check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=invoke();assert result.snapshot_id==snapshot and result.saved_response_sha256==sha256(raw).hexdigest()
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls

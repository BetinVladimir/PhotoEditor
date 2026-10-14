from contextlib import contextmanager
from types import SimpleNamespace as NS
from datetime import datetime,timedelta,timezone
from hashlib import sha256
from uuid import uuid4
import base64
import hashlib
import hmac
import psycopg
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_intent_catalogue as m
from photo_worker.runtime.restore_inventory import RestoreObject


def independent_mac(key,purpose,value):
    fields=[v.encode('utf-8') for v in (purpose,value)]
    message=b''.join(len(v).to_bytes(8,'big')+v for v in fields)
    return hmac.new(key,message,hashlib.sha256).digest()


@pytest.mark.parametrize('mode',['valid','authority','receipt','token','hash','artifact','expiry','request','missing','inactive'])
@pytest.mark.parametrize('consumed',[False,True])
def test_owner_intent_saved_receipt_and_exact_volume_binding(mode,consumed):
    actor,intent,artifact=uuid4(),uuid4(),uuid4();pepper=b'p'*32
    target=m.RestoreDownloadIntentTarget('https://restore.test',uuid4(),uuid4(),1,'fresh','Bearer secret')
    expires=datetime.now(timezone.utc)+timedelta(seconds=50)
    token=base64.urlsafe_b64encode(independent_mac(pepper,'portal-download-bearer',str(intent))).rstrip(b'=').decode()
    raw=canonical_bytes({'intent_id':str(intent),'expires_at':expires.isoformat()})
    fingerprint=sha256(canonical_bytes({'download_job_id':str(target.download_job_id),'volume':1})).hexdigest()
    ticket=m.RestoreDownloadIntentTicket('x'*43 if mode=='token' else token,expires,
        '0'*64 if mode=='request' else sha256(m.download_intent_request_body(target)).hexdigest(),'a'*64)
    volume=m.RestoreExportVolume(artifact,RestoreObject('photo','tenant/file','v1','b'*64,12),None,None,'single')
    calls=[]
    class Connection:
        info=NS(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):calls.append('sql');yield
        def execute(self,sql,args=None):
            if sql.startswith('SELECT current_database'):row=('restore',mode!='authority')
            elif sql.startswith('SELECT request_hash'):
                assert args==(target.tenant_id,actor,'fresh')
                row=(fingerprint,200 if mode=='receipt' else 201,raw,intent)
            elif sql.startswith('SELECT i.artifact_id'):
                assert args==(target.tenant_id,intent,actor,target.download_job_id,1,consumed)
                assert "(i.consumed_at IS NOT NULL)=%s" in sql and "i.intent_kind='volume'" in sql
                assert "j.principal_id=i.principal_id" in sql and "i.expires_at>clock_timestamp()" in sql
                row=None if mode=='missing' else (uuid4() if mode=='artifact' else artifact,
                    b'wrong' if mode=='hash' else independent_mac(pepper,'portal-download-intent',token),
                    expires+timedelta(seconds=1) if mode=='expiry' else expires)
            else:row=None
            return NS(fetchone=lambda:row)
    def invoke():return m.observe_fixture_owner_intent(Connection(),target,ticket,actor,volume,
        expected_database='restore',pepper=pepper,check_active=lambda:False if mode=='inactive' else None,
        consumed=consumed)
    if mode=='valid':
        result=invoke()
        assert result.intent_id==intent and result.artifact_id==artifact
        assert result.saved_response_sha256==sha256(raw).hexdigest()
        assert token not in repr(result) and 'secret' not in repr(result)
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    if mode=='inactive':assert not calls


def test_mac_length_prefix_prevents_ambiguous_field_concatenation():
    key=b'p'*32
    assert m._mac(key,'ab','c')==independent_mac(key,'ab','c')
    assert m._mac(key,'ab','c')!=m._mac(key,'a','bc')
    assert m._mac(key,'purpose','токен')==independent_mac(key,'purpose','токен')

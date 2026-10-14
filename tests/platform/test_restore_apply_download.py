from io import BytesIO
from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
from datetime import datetime,timezone,timedelta
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_apply_download as m


@pytest.mark.parametrize('mode',['valid','missing','duplicate','hash','foreign','version','wrong_key','corrupt','redirect','too_long','expired_before_get','scope_false','activity_false'])
def test_exact_adopted_apply_document_download(monkeypatch,mode):
    tenant,run,artifact=uuid4(),uuid4(),uuid4();body=b'{"working_image":"fixture"}';digest=sha256(body).hexdigest()
    result=NS(registration=NS(run_id=run),artifact_id=artifact,object=NS(bucket='bucket',key='object',sha256=digest,size=len(body),version='v1'))
    now=datetime.now(timezone.utc)
    expiry='2099-01-01T00:00:00Z' if mode=='too_long' else (now+timedelta(minutes=1 if mode=='expired_before_get' else 5)).isoformat()
    item={'artifact_id':str(uuid4() if mode=='missing' else artifact),'role':'working_image',
        'sha256':'f'*64 if mode=='hash' else digest,'bytes':len(body),'media_type':'application/json',
        'url':'https://'+('foreign.test' if mode=='foreign' else 'objects.test')+'/bucket/'+('other' if mode=='wrong_key' else 'object')+'?versionId='+('v2' if mode=='version' else 'v1'),
        'expires_at':expiry}
    raw=canonical_bytes({'items':[item,item] if mode=='duplicate' else [item]});calls=[]
    class Connection:
        def __init__(self,host,*a,**k):self.host=host
        def request(self,method,path,headers):
            calls.append(self.host)
            if self.host=='objects.test':assert 'Authorization' not in headers
        def getresponse(self):
            stream=BytesIO(raw if self.host=='api.test' else b'bad' if mode=='corrupt' else body)
            return NS(status=302 if mode=='redirect' else 200,read=stream.read,
                getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):pass
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    if mode=='expired_before_get':
        class Clock:
            calls=0
            @staticmethod
            def fromisoformat(value):return datetime.fromisoformat(value)
            @classmethod
            def now(cls,tz):
                cls.calls+=1
                return now+(timedelta(minutes=2) if cls.calls>=2 else timedelta())
        monkeypatch.setattr(m,'datetime',Clock)
    def invoke():return m.verify_fixture_apply_download(m.RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private'),
        tenant,result,tls_context=ssl.create_default_context(),
        assert_fixture_scope=lambda *a:False if mode=='scope_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke().sha256==digest and len(calls)==2
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert 'foreign.test' not in calls and len(calls)<=2
    if mode in ('scope_false','activity_false'):assert calls==[]

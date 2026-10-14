from types import SimpleNamespace as NS
from uuid import uuid4
from hashlib import sha256
from datetime import datetime,timezone,timedelta
from io import BytesIO
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_preview_download as m


@pytest.mark.parametrize('mode',['valid','foreign_host','version','wrong_key','bytes','redirect','corrupt','expired','too_long','expires_between_gets','manifest_sha','manifest_id','manifest_host','duplicate','fixture_false','activity_false'])
def test_outputs_signed_download_is_pinned_and_never_forwards_bearer(monkeypatch,mode):
    tenant,run,artifact=uuid4(),uuid4(),uuid4();body=b'jpeg-fixture';digest=sha256(body).hexdigest()
    manifest_id=uuid4();manifest_body=b'{"preview":true}';manifest_sha=sha256(manifest_body).hexdigest()
    result=NS(registration=NS(run_id=run),derivative=NS(artifact_id=artifact,role='derivative',object=NS(bucket='bucket',key='object',sha256=digest,size=len(body),version='v1')),
        manifest=NS(artifact_id=manifest_id,role='derivative_manifest',object=NS(bucket='bucket',key='manifest',sha256=manifest_sha,size=len(manifest_body),version='v2')))
    now=datetime.now(timezone.utc)
    expiry=('2000-01-01T00:00:00Z' if mode=='expired' else
        '2099-01-01T00:00:00Z' if mode=='too_long' else
        (now+timedelta(minutes=1 if mode=='expires_between_gets' else 5)).isoformat())
    item={'artifact_id':str(artifact),'role':'derivative','sha256':digest,'bytes':99 if mode=='bytes' else len(body),
        'media_type':'image/jpeg','url':'https://'+('foreign.test' if mode=='foreign_host' else 'objects.test')+
        '/bucket/'+('other' if mode=='wrong_key' else 'object')+'?versionId='+('v2' if mode=='version' else 'v1')+'&X-Amz-Signature=secret',
        'expires_at':expiry}
    manifest_item={'artifact_id':str(uuid4() if mode=='manifest_id' else manifest_id),'role':'derivative_manifest',
        'sha256':'f'*64 if mode=='manifest_sha' else manifest_sha,'bytes':len(manifest_body),'media_type':'application/json',
        'url':'https://'+('foreign.test' if mode=='manifest_host' else 'objects.test')+'/bucket/manifest?versionId=v2',
        'expires_at':expiry}
    raw=canonical_bytes({'items':[item,item if mode=='duplicate' else manifest_item]});calls=[]
    class Connection:
        def __init__(self,host,port,**kwargs):self.host=host
        def request(self,method,path,headers):
            calls.append(self.host);self.is_manifest='/manifest?' in path
            assert method=='GET'
            if self.host=='objects.test':assert 'Authorization' not in headers
            else:assert headers['Authorization']=='Bearer private'
        def getresponse(self):
            is_api=self.host=='api.test';stream=BytesIO(raw if is_api else b'corrupted!' if mode=='corrupt' else manifest_body if self.is_manifest else body)
            return NS(status=302 if mode=='redirect' else 200,read=stream.read,
                getheader=lambda name,default=None:('application/json' if is_api or self.is_manifest else 'image/jpeg') if name=='Content-Type' else default)
        def close(self):pass
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    if mode=='expires_between_gets':
        class Clock:
            calls=0
            @staticmethod
            def fromisoformat(value):return datetime.fromisoformat(value)
            @classmethod
            def now(cls,tz):
                cls.calls+=1
                return now+(timedelta(minutes=2) if cls.calls>=4 else timedelta())
        monkeypatch.setattr(m,'datetime',Clock)
    def invoke():return m.verify_fixture_preview_download(m.RestorePreviewDownloadTarget('https://api.test','https://objects.test','Bearer private'),
        tenant,result,tls_context=ssl.create_default_context(),
        assert_fixture_scope=lambda *a:False if mode=='fixture_false' else None,
        check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert invoke().sha256==digest
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert 'foreign.test' not in calls and len(calls)<=3

    if mode in ('wrong_key','manifest_sha','manifest_id','manifest_host','duplicate'):assert calls==['api.test']
    if mode in ('fixture_false','activity_false'):assert calls==[]

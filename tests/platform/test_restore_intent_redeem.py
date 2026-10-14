from datetime import datetime,timedelta,timezone
from hashlib import sha256
from io import BytesIO
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_intent_redeem as m
from photo_worker.runtime.restore_download_intent import RestoreDownloadIntentTicket
from photo_worker.runtime.restore_intent_catalogue import RestoreIntentRegistration
from photo_worker.runtime.restore_inventory import RestoreObject
from photo_worker.runtime.restore_preview_download import RestorePreviewDownloadTarget


@pytest.mark.parametrize('mode',['valid','origin','version','path','sha','bytes','expired','redirect','corrupt','truncated','oversize','network','inactive','fixture_denied'])
def test_redeem_streams_exact_volume_and_never_forwards_owner_bearer(monkeypatch,mode):
    target=RestorePreviewDownloadTarget('https://restore.test','https://objects.test','Bearer private')
    artifact=uuid4();data=b'encoded final image';digest=sha256(data).hexdigest()
    volume=m.RestoreExportVolume(artifact,RestoreObject('photo','tenant/file','v1',digest,len(data)),None,None,'single')
    intent=m.RestoreRegisteredIntent(RestoreDownloadIntentTicket('x'*43,datetime.now(timezone.utc)+timedelta(seconds=50),'a'*64,'b'*64),
        RestoreIntentRegistration(uuid4(),artifact,'c'*64))
    url='https://objects.test/photo/tenant/file?versionId=v1'
    if mode=='origin':url=url.replace('objects.test','foreign.test')
    if mode=='version':url=url.replace('v1','v2')
    if mode=='path':url=url.replace('/tenant/file','/tenant/other')
    value={'url':url,'expires_at':'2000-01-01T00:00:00Z' if mode=='expired' else '2099-01-01T00:00:00Z',
        'filename':'image.jpg','bytes':len(data)+1 if mode=='bytes' else len(data),'sha256':'0'*64 if mode=='sha' else digest}
    calls=[];closed=[]
    class Connection:
        def __init__(self,host,*args,**kwargs):self.host=host;assert kwargs['timeout']==5
        def request(self,method,path,body=None,headers=None):
            calls.append((method,self.host,path));assert 'Authorization' not in headers
            if method=='POST':assert parse_json(body)=={'token':intent.ticket.token}
            if mode=='network':raise OSError('secret')
        def getresponse(self):
            payload=canonical_bytes(value) if self.host=='restore.test' else (
                b'z'*len(data) if mode=='corrupt' else data[:-1] if mode=='truncated' else data+b'z' if mode=='oversize' else data)
            stream=BytesIO(payload)
            return NS(status=302 if mode=='redirect' else 200,read=stream.read,
                getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):closed.append(self.host)
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    def invoke():return m.verify_fixture_intent_delivery(target,intent,volume,tls_context=ssl.create_default_context(),
        assert_fixture_scope=lambda *_:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        result=invoke();assert result.bytes_verified==len(data) and result.sha256==digest
        assert intent.ticket.token not in repr(result) and url not in repr(result)
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert len(closed)==len(calls)
    assert sum(method=='POST' for method,_,_ in calls)==(0 if mode in ('inactive','fixture_denied') else 1)
    if mode in ('origin','version','path','sha','bytes','expired','redirect','network'):assert len(calls)==1

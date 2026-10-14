from dataclasses import replace
from datetime import datetime,timezone,timedelta
from hashlib import sha256
import ssl
from urllib.parse import urlencode
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_s3_probe as module


@pytest.mark.parametrize('mode',['valid','expired','foreign_principal','foreign_version','wrong_origin',
    'access_denied','signature_expired','success','redirect','server_error','xml_entity','duplicate_code',
    'wrong_control_version','wrong_control_bytes','network_error','scope_error','activity_false','scope_false','baseline_valid','baseline_missing','baseline_wrong_version','baseline_wrong_bytes'])
def test_signed_s3_denial_requires_fresh_exact_version_and_live_control(monkeypatch,mode):
    tenant=uuid4();date=datetime.now(timezone.utc)-timedelta(seconds=1)
    if mode=='expired':date-=timedelta(hours=1)
    origin='https://restore.example.test';key='tenants/'+str(tenant)+'/removed'
    control_key='tenants/'+str(uuid4())+'/control';control=b'live fixture bytes'
    def url(key,version,principal='fixture'):
        query={'X-Amz-Algorithm':'AWS4-HMAC-SHA256','X-Amz-Credential':principal+'/'+date.strftime('%Y%m%d')+'/test/s3/aws4_request',
            'X-Amz-Date':date.strftime('%Y%m%dT%H%M%SZ'),'X-Amz-Expires':'600','X-Amz-SignedHeaders':'host',
            'X-Amz-Signature':'a'*64,'versionId':version}
        return origin+'/test-bucket/'+key+'?'+urlencode(query)
    target=module.RestoreS3AccessTarget(origin,'test-bucket',tenant,key,'removed-v1',control_key,'live-v1',
        sha256(control).hexdigest(),url(key,'wrong' if mode=='foreign_version' else 'removed-v1'),
        url(control_key,'live-v1','other' if mode=='foreign_principal' else 'fixture'))
    if mode=='wrong_origin':target=replace(target,denied_url=target.denied_url.replace('restore.example.test','foreign.example.test'))
    calls=[];closed=[]
    class Connection:
        def __init__(self,host,port,**kwargs):assert host=='restore.example.test' and port==443
        def request(self,method,path,headers):
            assert method=='GET' and 'Authorization' not in headers;calls.append(path)
            if mode=='network_error':raise OSError('sensitive signed url')
        def getresponse(self):
            positive='/control?' in calls[-1]
            status=200 if positive else 404
            raw=control if positive else b'<Error><Code>NoSuchVersion</Code></Error>'
            media='application/octet-stream' if positive else 'application/xml';version='live-v1' if positive else None
            if positive:
                if mode=='wrong_control_version':version='other'
                if mode=='wrong_control_bytes':raw=b'changed'
            else:
                status={'access_denied':403,'signature_expired':403,'success':200,'redirect':302,'server_error':503}.get(mode,status)
                if mode.startswith('baseline_') and mode!='baseline_missing':
                    status=200;raw=b'pre-purge';version='wrong' if mode=='baseline_wrong_version' else 'removed-v1'
                if mode=='xml_entity':raw=b'<!DOCTYPE Error [<!ENTITY x "NoSuchVersion">]><Error><Code>&x;</Code></Error>'
                if mode=='duplicate_code':raw=b'<Error><Code>NoSuchVersion</Code><Code>AccessDenied</Code></Error>'
            class Response:
                def read(self,size):return raw
                def getheader(self,name,default=None):return {'Content-Type':media,'x-amz-version-id':version}.get(name,default)
            result=Response();result.status=status;return result
        def close(self):closed.append(True)
    monkeypatch.setattr(module.http.client,'HTTPSConnection',Connection)
    def scope(actual):
        assert actual is target
        if mode=='scope_error':raise RuntimeError('wrong fixture')
        if mode=='scope_false':return False
    def run():
        if mode.startswith('baseline_'):
            pin=sha256(b'pre-purge').hexdigest() if mode!='baseline_wrong_bytes' else '0'*64
            return module.observe_signed_s3_readable(target,pin,len(b'pre-purge'),tls_context=ssl.create_default_context(),assert_fixture_scope=scope,check_active=lambda:None)
        return module.observe_purged_signed_s3(target,tls_context=ssl.create_default_context(),assert_fixture_scope=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode=='baseline_valid':
        result=run();assert result.schema_version=='restore-s3-readable/v1' and len(calls)==3
    elif mode=='valid':
        result=run();assert result.code=='NoSuchVersion' and result.version=='removed-v1'
        assert len(calls)==3
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert len(closed)==len(calls)
    if mode in ('activity_false','scope_false'):assert calls==[]
    assert 'X-Amz-Signature' not in repr(target)

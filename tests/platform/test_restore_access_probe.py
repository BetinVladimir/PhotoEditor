import ssl
from hashlib import sha256
import pytest
from photo_worker.runtime import restore_access_probe as module


@pytest.mark.parametrize('mode',['valid','success_response','unauthorized','server_error','redirect',
    'wrong_code','xml','oversized','compressed','control_before','control_after','network_error','scope_error','activity_false','scope_false','baseline_valid','baseline_denied','baseline_wrong_bytes'])
def test_https_denial_requires_live_controls_and_exact_domain_error(monkeypatch,mode):
    calls=[];closed=[]
    class Connection:
        def __init__(self,host,port,**kwargs):assert host=='restore.example.test' and port==443
        def request(self,method,path,headers):
            assert method=='GET' and headers['Authorization']=='Bearer private-fixture'
            calls.append(path)
            if mode=='network_error':raise OSError('private endpoint')
        def getresponse(self):
            control=calls[-1]=='/control';status=200 if control else 403
            raw=b'{"id":"control"}' if control else b'{"code":"FORBIDDEN"}';media='application/json';encoding=None
            if not control:
                status={'success_response':200,'unauthorized':401,'server_error':503,'redirect':302}.get(mode,status)
                if mode.startswith('baseline_') and mode!='baseline_denied':
                    status=200;raw=b'{"id":"before-purge"}'
                if mode=='wrong_code':raw=b'{"code":"INVALID_CREDENTIALS"}'
                if mode=='xml':media='application/xml'
                if mode=='oversized':raw=b'x'*16385
                if mode=='compressed':encoding='gzip'
            if control and ((mode=='control_before' and len(calls)==1) or (mode=='control_after' and len(calls)==3)):status=401
            class Response:
                def read(self,size):assert size==16385;return raw
                def getheader(self,name,default=None):return {'Content-Type':media,'Content-Encoding':encoding}.get(name,default)
            response=Response();response.status=status;return response
        def close(self):closed.append(True)
    monkeypatch.setattr(module.http.client,'HTTPSConnection',Connection)
    target=module.RestoreAccessTarget('https://restore.example.test','/denied','/control','Bearer private-fixture',403,'FORBIDDEN',sha256(b'{"id":"control"}').hexdigest())
    def scope(actual):
        assert actual is target
        if mode=='scope_error':raise RuntimeError('scope changed')
        if mode=='scope_false':return False
    def run():
        if mode.startswith('baseline_'):
            pin=sha256(b'{"id":"before-purge"}').hexdigest() if mode!='baseline_wrong_bytes' else '0'*64
            return module.observe_restore_access_readable(target,pin,tls_context=ssl.create_default_context(),assert_fixture_scope=scope,check_active=lambda:None)
        return module.observe_restore_access_denied(target,tls_context=ssl.create_default_context(),assert_fixture_scope=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode=='baseline_valid':
        result=run();assert result.schema_version=='restore-access-readable/v1' and len(calls)==3
    elif mode=='valid':
        result=run();assert result.code=='FORBIDDEN' and len(result.response_sha256)==64
        assert calls==['/control','/denied','/control']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert len(closed)==len(calls)
    if mode in ('activity_false','scope_false'):assert calls==[]
    assert 'private-fixture' not in repr(target)

from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_download_intent as m


@pytest.mark.parametrize('mode',['valid','token','expired','extra','redirect','network','inactive','fixture_denied'])
def test_owner_intent_native_contract_no_retry_no_token_in_repr(monkeypatch,mode):
    target=m.RestoreDownloadIntentTarget('https://restore.test',uuid4(),uuid4(),1,'fresh','Bearer private')
    token='a'*43;value={'intent_token':'bad!' if mode=='token' else token,
        'expires_at':'2000-01-01T00:00:00Z' if mode=='expired' else '2099-01-01T00:00:00Z'}
    if mode=='extra':value['extra']=True
    calls=[]
    class Connection:
        def __init__(self,*a,**k):assert k['timeout']==5
        def request(self,method,path,body,headers):
            calls.append(path);assert parse_json(body)=={'volume':1} and path.endswith(f'/{target.download_job_id}/download-intent')
            if mode=='network':raise OSError('private')
        def getresponse(self):return NS(status=302 if mode=='redirect' else 201,read=lambda _:canonical_bytes(value),
            getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):pass
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    def invoke():return m.request_fixture_download_intent(target,tls_context=ssl.create_default_context(),
        assert_fixture_scope=lambda _:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':
        ticket=invoke();assert ticket.token==token and token not in repr(ticket)
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert len(calls)==(0 if mode in ('inactive','fixture_denied') else 1)

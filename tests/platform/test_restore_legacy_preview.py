from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_legacy_preview as m


@pytest.mark.parametrize('mode',['valid','replay','wrong_status','zero_run','extra','network','redirect','scope','fixture_false','activity_false'])
def test_legacy_preview_native_one_item_ticket_is_not_completion_and_never_retried(monkeypatch,mode):
    target=m.RestoreLegacyPreviewTarget('https://restore.test',uuid4(),uuid4(),7,canonical_bytes({'strength':1}),'fresh','Bearer private')
    value={'run_id':str(__import__('uuid').UUID(int=0) if mode=='zero_run' else uuid4()),
        'status':'succeeded' if mode=='wrong_status' else 'queued'}
    if mode=='extra':value['extra']=True
    calls=[];closed=[]
    class Connection:
        def __init__(self,*a,**k):assert k['timeout']==5
        def request(self,method,path,body,headers):
            calls.append(path)
            assert method=='POST' and path.endswith(f'/tenants/{target.tenant_id}/previews')
            assert parse_json(body)=={'asset_version_id':str(target.asset_version_id),'draft_revision':7,
                'params':{'strength':1},'scale':'proxy','generation':False}
            assert headers['X-Photo-Client']=='native'
            if mode=='network':raise OSError('private')
        def getresponse(self):return NS(status=302 if mode=='redirect' else 202,read=lambda _:canonical_bytes(value),
            getheader=lambda name,default=None:'application/json' if name=='Content-Type' else
                'true' if name=='Idempotency-Replayed' and mode=='replay' else default)
        def close(self):closed.append(True)
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    def scope(t):
        assert t is target
        if mode=='scope':raise ValueError('lost isolation')
        if mode=='fixture_false':return False
    def run():return m.request_fixture_legacy_preview(target,tls_context=ssl.create_default_context(),
        assert_fixture_scope=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert str(run().run_id)==value['run_id']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert len(calls)<=1 and len(calls)==len(closed)
    if mode in ('fixture_false','activity_false'):assert calls==[]


@pytest.mark.parametrize('change',[
    {'draft_revision':True},{'draft_revision':-1},{'scale':'thumbnail'},
    {'params_raw':b'{}'},{'params_raw':b'{"unknown":1}'},
    {'params_raw':b'{"strength": 1}'},{'params_raw':b'{"schema_version":true,"modules":{"strength":1}}'},
    {'roi':(0,0,0,1)},{'roi':(False,0,1,1)},
])
def test_preview_invalid_request_never_opens_transport(monkeypatch,change):
    from dataclasses import replace
    target=m.RestoreLegacyPreviewTarget('https://restore.test',uuid4(),uuid4(),0,
        canonical_bytes({'strength':1}),'fresh','Bearer private')
    def forbidden(*a,**k):raise AssertionError('network before validation')
    monkeypatch.setattr(m.http.client,'HTTPSConnection',forbidden)
    with pytest.raises(ValueError):
        m.request_fixture_legacy_preview(replace(target,**change),tls_context=ssl.create_default_context(),
            assert_fixture_scope=lambda _:None,check_active=lambda:None)


def test_preview_optional_pins_are_serialized_and_secrets_hidden():
    target=m.RestoreLegacyPreviewTarget('https://restore.test',uuid4(),uuid4(),0,
        canonical_bytes({'schema_version':1,'modules':{'global':{}}}),'secret-key','Bearer secret-token',
        uuid4(),(0,0,32,64),'full')
    body=parse_json(m.legacy_preview_request_body(target))
    assert body['style_version_id']==str(target.style_version_id)
    assert body['roi']==[0,0,32,64] and body['generation'] is False
    assert 'secret-key' not in repr(target) and 'secret-token' not in repr(target)

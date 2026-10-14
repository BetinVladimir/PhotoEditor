from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_selection as m


@pytest.mark.parametrize('mode',['valid','count','digest','id','expired','extra','redirect','network','revision','inactive','fixture_denied'])
def test_native_selection_one_post_closed_contract(monkeypatch,mode):
    target=m.RestoreSelectionTarget('https://restore.test',uuid4(),uuid4(),uuid4(),0 if mode=='revision' else 7,'fresh','Bearer secret')
    value={'id':'bad' if mode=='id' else str(uuid4()),'digest':'bad' if mode=='digest' else 'a'*64,
        'item_count':True if mode=='count' else 1,'expires_at':'2000-01-01T00:00:00Z' if mode=='expired' else '2099-01-01T00:00:00Z'}
    if mode=='extra':value['extra']=True
    calls=[];closed=[]
    class Connection:
        def __init__(self,*args,**kwargs):assert kwargs['timeout']==5
        def request(self,method,path,body,headers):
            calls.append(path)
            assert method=='POST' and path.endswith(f'/{target.collection_id}/selection-snapshots')
            assert parse_json(body)=={'collection_revision':'7','scope':'selected','item_ids':[str(target.item_id)]}
            if mode=='network':raise OSError('secret')
        def getresponse(self):return NS(status=302 if mode=='redirect' else 201,read=lambda _:canonical_bytes(value),
            getheader=lambda name,default=None:'application/json' if name=='Content-Type' else default)
        def close(self):closed.append(True)
    monkeypatch.setattr(m.http.client,'HTTPSConnection',Connection)
    def invoke():return m.request_fixture_selection(target,tls_context=ssl.create_default_context(),
        assert_fixture_scope=lambda _:False if mode=='fixture_denied' else None,
        check_active=lambda:False if mode=='inactive' else None)
    if mode=='valid':assert invoke().digest=='a'*64
    else:
        with pytest.raises((ValueError,RuntimeError)):invoke()
    assert len(calls)==(0 if mode in ('revision','inactive','fixture_denied') else 1) and len(closed)==len(calls)

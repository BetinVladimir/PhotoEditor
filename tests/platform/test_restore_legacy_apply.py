from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime import restore_legacy_apply as m


@pytest.mark.parametrize('mode',['valid','replay','wrong_status','source_output','extra','network','redirect','scope','scope_false','activity_false'])
def test_legacy_apply_native_one_item_ticket_is_not_completion_and_never_retried(monkeypatch,mode):
    target=m.RestoreLegacyApplyTarget('https://restore.test',uuid4(),uuid4(),uuid4(),uuid4(),7,'fresh','Bearer private')
    value={'run_id':str(uuid4()),'output_collection_id':str(target.collection_id if mode=='source_output' else uuid4()),
        'status':'succeeded' if mode=='wrong_status' else 'queued'}
    if mode=='extra':value['extra']=True
    calls=[];closed=[]
    class Connection:
        def __init__(self,*a,**k):assert k['timeout']==5
        def request(self,method,path,body,headers):
            calls.append(path)
            assert method=='POST' and path.endswith(f'/collections/{target.collection_id}/apply-profile')
            assert parse_json(body)=={'source_revision':7,'style_version_id':str(target.style_version_id),
                'scope':'selected','item_ids':[str(target.item_id)]}
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
        if mode=='scope_false':return False
    def run():return m.request_fixture_legacy_apply(target,tls_context=ssl.create_default_context(),
        assert_fixture_scope=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert str(run().run_id)==value['run_id']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert len(calls)<=1 and len(calls)==len(closed)
    if mode in ('scope_false','activity_false'):assert calls==[]

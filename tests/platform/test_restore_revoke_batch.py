from types import SimpleNamespace as NS
from uuid import uuid4
from datetime import datetime, timezone
import ssl
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.runtime import restore_revoke_batch as module


@pytest.mark.parametrize('mode',['valid','accepted','expired','unauthorized','unavailable','wrong_code','generic_validation','replay','network','scope','scope_false','activity_false'])
def test_exact_frozen_selection_native_batch_request_never_retries(monkeypatch,mode):
    t=module.RestoreRenderBatchTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'a'*64,'fresh-batch','Bearer secret')
    b=module.RestoreFreezeBaselineBinding(uuid4(),uuid4(),uuid4(),'b'*64,'c'*64)
    f=module.RestoreFreezeBaselineEvidence('restore-freeze-baseline/v1',uuid4(),uuid4(),'d'*64,'e'*64,'f'*64,datetime.now(timezone.utc))
    calls=[];closed=[]
    class Connection:
        def __init__(self,*args,**kwargs):assert kwargs['timeout']==5
        def request(self,method,path,body,headers):
            calls.append(path)
            assert method=='POST' and path==f'/api/v1/tenants/{t.tenant_id}/projects/{t.project_id}/render-batches'
            value=parse_json(body)
            assert value['input_snapshot_id']==str(t.snapshot_id) and value['policy_sha']==t.policy_sha256
            assert len(value['selection'])==1 and value['selection'][0]['recipe']==dict(artifact_id=str(f.recipe_artifact_id),sha256=f.recipe_sha256)
            assert headers['X-Photo-Client']=='native' and headers['Idempotency-Key']=='fresh-batch'
            if mode=='network':raise OSError('secret')
        def getresponse(self):return NS(status={'accepted':202,'expired':410,'unauthorized':401,'unavailable':503,'generic_validation':422}.get(mode,409),
            read=lambda _:b'{"code":"FORBIDDEN"}' if mode=='wrong_code' else b'{"code":"VALIDATION_FAILED"}' if mode=='generic_validation' else b'{"code":"PROFILE_CONSENT_REVOKED"}',
            getheader=lambda name,default=None:'application/json' if name=='Content-Type' else
            'true' if mode=='replay' and name=='Idempotency-Replayed' else default)
        def close(self):closed.append(True)
    monkeypatch.setattr(module.http.client,'HTTPSConnection',Connection)
    def scope(*args):
        assert args==(t,b,f)
        if mode=='scope':raise RuntimeError('scope lost')
        if mode=='scope_false':return False
    def run():return module.observe_fixture_render_batch_refused(t,b,f,tls_context=ssl.create_default_context(),
        assert_current_fixture=scope,check_active=lambda:False if mode=='activity_false' else None)
    if mode=='valid':assert run().code=='PROFILE_CONSENT_REVOKED'
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
    assert len(calls)<=1 and len(calls)==len(closed)
    if mode in ('scope_false','activity_false'):assert calls==[]

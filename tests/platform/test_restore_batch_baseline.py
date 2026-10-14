from datetime import datetime,timezone
from types import SimpleNamespace as NS
from uuid import uuid4,uuid5
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_revoke_batch as module


@pytest.mark.parametrize('mode',['valid','running','foreign_batch','foreign_child','extra_child','bad_state','extra_field','refused','replay'])
def test_positive_batch_requires_exact_one_frame_deterministic_ticket(monkeypatch,mode):
    t=module.RestoreRenderBatchTarget('https://restore.test',uuid4(),uuid4(),uuid4(),'a'*64,'positive','Bearer private')
    b=module.RestoreFreezeBaselineBinding(uuid4(),uuid4(),uuid4(),'b'*64,'c'*64)
    f=module.RestoreFreezeBaselineEvidence('restore-freeze-baseline/v1',uuid4(),uuid4(),'d'*64,'e'*64,'f'*64,datetime.now(timezone.utc))
    batch=uuid5(t.tenant_id,'V2.Admit:'+str(b.actor_id)+':positive');child=uuid5(batch,'render.batch:0')
    ticket=dict(run_id=str(batch),state='running' if mode=='running' else 'queued',child_run_ids=[str(child)])
    if mode=='foreign_batch':ticket['run_id']=str(uuid4())
    if mode=='foreign_child':ticket['child_run_ids']=[str(uuid4())]
    if mode=='extra_child':ticket['child_run_ids'].append(str(uuid5(batch,'render.batch:1')))
    if mode=='bad_state':ticket['state']='succeeded'
    if mode=='extra_field':ticket['extra']=True
    calls=[];closed=[]
    class Connection:
        def __init__(self,*args,**kwargs):pass
        def request(self,method,path,body,headers):calls.append(path);assert method=='POST'
        def getresponse(self):return NS(status=422 if mode=='refused' else 202,read=lambda _:canonical_bytes(ticket),
            getheader=lambda name,default=None:'application/json' if name=='Content-Type' else
            'true' if mode=='replay' and name=='Idempotency-Replayed' else default)
        def close(self):closed.append(True)
    monkeypatch.setattr(module.http.client,'HTTPSConnection',Connection)
    def run():return module.observe_fixture_render_batch_baseline(t,b,f,tls_context=ssl.create_default_context(),
        assert_current_fixture=lambda *args:None,check_active=lambda:None)
    if mode in ('valid','running'):
        result=run();assert result.batch_id==batch and result.child_run_id==child
    else:
        with pytest.raises(ValueError):run()
    assert len(calls)==1 and closed==[True]

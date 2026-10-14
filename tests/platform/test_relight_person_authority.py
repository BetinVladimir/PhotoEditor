from types import SimpleNamespace
from uuid import uuid4
from hashlib import sha256
import pytest
from test_relight_inputs import inputs
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.person import PersonParams
from photo_worker.operations.portrait.relight_person_authority import SQLRelightPersonAuthority

@pytest.mark.parametrize('mode',['success','source','sha','undeclared','changed','fence'])
def test_exact_person_producer_replay_and_recheck(mode):
    params,source,_,subject,*_=inputs()
    producer=PersonParams(image=params.base.source,model_lock_id='a'*64)
    if mode=='source':producer=producer.model_copy(update={'image':ArtifactRef(artifact_id=uuid4(),sha256='a'*64)})
    raw=canonical_bytes(producer);ref=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(worker_id=uuid4(),fence=1,
        input_digest='a'*64,execution_profile={},command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),
        task_id=uuid4(),attempt=1,params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params)),
        inputs=[] if mode=='undeclared' else [ref])))
    record=dict(**ref.model_dump(mode='json'),bucket='test',object_key='producer',object_version='v1',bytes=len(raw),media_type='application/json')
    calls=[];reads=[]
    def lookup(*args):
        calls.append(args)
        value=dict(record)
        if mode=='changed' and len(calls)>1:value['object_version']='v2'
        if mode=='fence' and len(calls)>1:ctx.lease.fence=2
        return value
    def read(*args):reads.append(args);return b'x'+raw[1:] if mode=='sha' else raw
    authority=SQLRelightPersonAuthority(SimpleNamespace(relight_person_origin=lookup),SimpleNamespace(contract_bytes=read))
    if mode=='success':
        assert authority.verify(ctx,params,source,subject) is True and len(calls)==2 and len(reads)==1
    else:
        with pytest.raises((ValueError,RuntimeError)):authority.verify(ctx,params,source,subject)
        if mode=='undeclared':assert not reads

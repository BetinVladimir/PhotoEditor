from types import SimpleNamespace
from uuid import uuid4
import json

import pytest

from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.contracts.portrait_local import PortraitHairParams
from photo_worker.operations.portrait.local_manual_admission import LeaseGatedManualPairAuthority
from photo_worker.operations.portrait.local_manual_inputs import ManualPairOrigin
from photo_worker.runtime.executor import OperationFailure


def setup():
    ref=lambda:ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=PortraitHairParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref().model_dump(mode='json'),'canvas_hash':'b'*64},'strength':1,'mask':ref().model_dump(mode='json'),'protected':ref().model_dump(mode='json'),'hue_delta_degrees':0,'saturation_factor':1,'detail':0}))
    command=SimpleNamespace(tenant_id=uuid4(),run_id=uuid4(),task_id=uuid4(),attempt=1,params=ArtifactRef(artifact_id=uuid4(),sha256=canonical_hash(params)))
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(command=command,worker_id=uuid4(),fence=5,input_digest='c'*64,execution_profile={'backend':'cpu','renderer_version':'v2.2','portrait_runtime_sha':'e'*64,'resource_estimate':{'ram_bytes':1024}}))
    return ctx,params,ManualPairOrigin(ref(),ref(),None)


@pytest.mark.parametrize('first,second',[(True,True),(False,True),(True,False),(1,True)])
def test_lease_gated_authority_refuses_before_or_after_lineage(first,second):
    ctx,params,origin=setup();events=[]
    class Database:
        def local_portrait_lease_ready(self,lease,body):
            assert lease is ctx.lease and body==canonical_bytes(params)
            events.append('lease')
            return first if len(events)==1 else second
    class Producer:
        def resolve(self,*args):events.append('origin');return origin
    authority=LeaseGatedManualPairAuthority(Database(),Producer())
    if first is True and second is True:
        assert authority.resolve(ctx,params,None,None,None)==origin
    else:
        with pytest.raises(OperationFailure,match='PORTRAIT_LEASE_NOT_READY'):
            authority.resolve(ctx,params,None,None,None)
    assert events==(['lease','origin','lease'] if first is True else ['lease'])


def test_lease_gated_authority_rejects_unbound_params_without_sql():
    ctx,params,origin=setup();ctx.lease.command.params=ArtifactRef(artifact_id=uuid4(),sha256='d'*64)
    class Database:
        def local_portrait_lease_ready(self,*args):raise AssertionError('unexpected SQL')
    authority=LeaseGatedManualPairAuthority(Database(),SimpleNamespace(resolve=lambda *args:origin))
    with pytest.raises(OperationFailure,match='PORTRAIT_PARAMS_BINDING_MISMATCH'):
        authority.resolve(ctx,params,None,None,None)


def test_lease_gated_authority_rejects_fence_change_during_lineage():
    ctx,params,origin=setup();calls=[]
    class Database:
        def local_portrait_lease_ready(self,*args):calls.append('lease');return True
    def resolve(*args):ctx.lease.fence+=1;return origin
    authority=LeaseGatedManualPairAuthority(Database(),SimpleNamespace(resolve=resolve))
    with pytest.raises(OperationFailure,match='PORTRAIT_MANUAL_ATTEMPT_MISMATCH'):
        authority.resolve(ctx,params,None,None,None)
    assert calls==['lease']



def test_manual_operation_factory_owns_admission_per_attempt(monkeypatch):
    from contextlib import contextmanager
    from photo_worker.operations.portrait import local_adapter
    from photo_worker.operations.portrait.local_manual_admission import manual_local_portrait_operations,ManualLocalPortraitAdmission
    from photo_worker.runtime.artifacts import TaskArtifacts
    instances=[];events=[]
    from photo_worker.operations.portrait import local_runtime_lock
    monkeypatch.setattr(local_runtime_lock,'verify_installed_runtime',lambda *args:'e'*64)
    @contextmanager
    def artifacts(ctx,config,roles):
        events.append('open')
        try:yield object()
        finally:events.append('close')
    monkeypatch.setattr(TaskArtifacts,'create',staticmethod(artifacts))
    def execute(ctx,artifacts,admission):
        assert isinstance(admission,ManualLocalPortraitAdmission)
        assert isinstance(admission.authority,LeaseGatedManualPairAuthority)
        instances.append(admission);return 'result'
    monkeypatch.setattr(local_adapter,'execute',execute)
    db=SimpleNamespace(local_portrait_lease_ready=lambda *args:True)
    operations=manual_local_portrait_operations(None,db,lambda *args:SimpleNamespace(resolve=lambda *args:None),object(),'e'*64)
    assert [op.name for op in operations]==['portrait.hair','portrait.clothing']
    for op in operations:
        ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(execution_profile={'portrait_runtime_sha':'e'*64}))
        assert op.execute(ctx)=='result'
    assert instances[0] is not instances[1]
    assert events==['open','close','open','close']



def test_lease_gated_authority_rejects_budget_change_during_lineage():
    ctx,params,origin=setup();calls=[]
    class Database:
        def local_portrait_lease_ready(self,*args):calls.append('lease');return True
    def resolve(*args):ctx.lease.execution_profile['resource_estimate']['ram_bytes']=2;return origin
    authority=LeaseGatedManualPairAuthority(Database(),SimpleNamespace(resolve=resolve))
    with pytest.raises(OperationFailure,match='PORTRAIT_MANUAL_ATTEMPT_MISMATCH'):
        authority.resolve(ctx,params,None,None,None)
    assert calls==['lease']

import hashlib
from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.series import RegionStats,SeriesParams,SeriesStats
from photo_worker.operations.series.balance import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.series_config import series_runtime_locks_from_env
from photo_worker.runtime.service import WorkerConfigurationError
from test_asset_adapters import context_for
import pytest
import numpy as np
from types import SimpleNamespace


def stats(rgb,confidence=1):
    return SeriesStats(item_id=uuid4(),image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),regions=[RegionStats(region_id='neutral-wall',mean_rgb=rgb,mean_y=sum(rgb)/3,coverage=.2,confidence=confidence)],clipping_fraction=0,warnings=[])


class Artifacts:
    def __init__(self,values,reference=None):
        self.contracts={};self.staged=[];refs=[]
        for value in values:r=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest());refs.append(r);self.contracts[r.artifact_id]=value
        self.value=SeriesParams(scene_id='scene-one',stats=refs,reference_item_id=reference)
    def params(self,model):return self.value
    def contract(self,r,model):return self.contracts[r.artifact_id]
    def stage_contract(self,value,role):self.staged.append((role,value));return ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def test_series_balance_applies_bounded_improving_correction():
    reference=stats([.4,.5,.6]);other=stats([.32,.4,.48]);a=Artifacts([reference,other],reference.item_id);ctx=context_for(a.value.stats[0],'series.balance');result=execute(ctx,a);correction=next(x for x in a.staged[0][1].items if x.item_id==other.item_id)
    assert correction.accepted and correction.reason=='applied' and abs(correction.applied_ev)<=.35
    assert all(.95<=v<=1.05 for v in correction.applied_gains) and not result.review_required


def test_series_balance_low_confidence_is_identity():
    reference=stats([.4,.5,.6]);other=stats([.3,.4,.5],.5);a=Artifacts([reference,other],reference.item_id);ctx=context_for(a.value.stats[0],'series.balance');result=execute(ctx,a);correction=next(x for x in a.staged[0][1].items if x.item_id==other.item_id)
    assert not correction.accepted and correction.reason=='low_confidence' and correction.applied_ev==0 and correction.applied_gains==[1,1,1]
    assert result.review_required


def test_qualified_series_requires_exact_installed_pins_before_artifacts():
    reference=stats([.4,.5,.6]);other=stats([.32,.4,.48]);a=Artifacts([reference,other],reference.item_id)
    ctx=context_for(a.value.stats[0],'series.balance')
    locks=('a'*64,'b'*64,'c'*64)
    ctx.lease.execution_profile.update(model_lock_sha=locks[0],runtime_lock_sha=locks[1],
        device_profile_sha=locks[2],model_lock_id=locks[1])
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='SERIES_RUNTIME_UNQUALIFIED' and not a.staged
    with pytest.raises(OperationFailure):execute(ctx,a,('d'*64,*locks[1:]))
    assert execute(ctx,a,locks).outputs and len(a.staged)==1


def test_series_deployment_pins_are_all_or_nothing():
    locks=('a'*64,'b'*64,'c'*64)
    env=dict(PHOTO_SERIES_RUNTIME_ENABLED='true',PHOTO_SERIES_MODEL_LOCK_SHA=locks[0],
        PHOTO_SERIES_RUNTIME_LOCK_SHA=locks[1],PHOTO_SERIES_DEVICE_PROFILE_SHA=locks[2])
    assert series_runtime_locks_from_env(env,'cpu','cpu')==locks
    with pytest.raises(WorkerConfigurationError):series_runtime_locks_from_env(env,'gpu','cuda')
    with pytest.raises(WorkerConfigurationError):series_runtime_locks_from_env(
        {**env,'PHOTO_SERIES_DEVICE_PROFILE_SHA':''},'cpu','cpu')
    with pytest.raises(WorkerConfigurationError):series_runtime_locks_from_env(
        {**env,'PHOTO_SERIES_RUNTIME_ENABLED':'false'},'cpu','cpu')
    assert series_runtime_locks_from_env({},'cpu','cpu') is None


@pytest.mark.parametrize('solution', [
    SimpleNamespace(success=False, x=np.zeros(4)),
    SimpleNamespace(success=True, x=np.array([float('nan'), 0, 0, 0])),
    SimpleNamespace(success=True, x=np.zeros(3)),
    SimpleNamespace(success=True, x=np.array([0, 1000, 0, 0])),
])
def test_series_solver_failure_keeps_identity(monkeypatch, solution):
    monkeypatch.setattr('photo_worker.operations.series.balance.least_squares',
                        lambda *args, **kwargs: solution)
    reference=stats([.4,.5,.6]);other=stats([.32,.4,.48]);a=Artifacts([reference,other],reference.item_id)
    ctx=context_for(a.value.stats[0],'series.balance')
    result=execute(ctx,a)
    correction=next(x for x in a.staged[0][1].items if x.item_id==other.item_id)
    assert correction.reason=='residual_not_improved'
    assert not correction.accepted and correction.applied_ev==0
    assert correction.applied_gains==[1,1,1] and result.review_required

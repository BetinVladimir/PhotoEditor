from uuid import uuid4
from photo_worker.contracts.base import ArtifactRef
"""Params reads must recheck activity before parsing or external authority IO."""
from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic import preview_removal_adopt_adapter as adapter
from photo_worker.operations.semantic import preview_removal_adopt_installation as installer
from photo_worker.runtime.executor import OperationFailure, TaskCancelled
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('boundary',['installer','adapter'])
@pytest.mark.parametrize('loss',['cancel','lease'])
def test_shown_removal_params_read_loss_prevents_parsing(monkeypatch,boundary,loss):
    expected=dict(model_lock_sha='a'*64,runtime_lock_sha='b'*64,device_profile_sha='c'*64,dataset_sha='d'*64)
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='semantic.adopt_shown_removal',operation_version='1',resource_class='cpu',params=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',qualification={},resource_estimate=estimate),fence=1))
    cancelled=False;reads=[]
    def active():
        if cancelled:raise TaskCancelled('cancelled during Params read')
    ctx.check_active=active
    def forbidden(*args,**kwargs):raise AssertionError('lost activity reached parsing or processing')
    ctx.progress=forbidden
    def read(*args):
        nonlocal cancelled
        reads.append('Params')
        if loss=='cancel':cancelled=True
        else:ctx.lease.fence+=1
        return b'not-json'
    artifacts=SimpleNamespace(contract_bytes=read)
    monkeypatch.setattr(installer,'ShownRemovalQualificationPin',SimpleNamespace(from_value=lambda value:SimpleNamespace(**expected)))
    monkeypatch.setattr(installer,'local_exposure_icc_verifier',lambda registry:forbidden)
    monkeypatch.setattr(installer,'_qualification_loader',forbidden)
    monkeypatch.setattr(adapter,'execute_shown_removal_adoption',forbidden)
    if boundary=='installer':
        execute=installer.sql_shown_removal_factory(database=SimpleNamespace(
            admit_semantic_shown_removal=forbidden,shown_removal_report_evidence=forbidden),
            objects=SimpleNamespace(get_object=forbidden),bucket='photos',icc_registry=object(),**expected)
        invoke=lambda:execute(ctx,artifacts)
    else:
        installation=adapter.ShownRemovalInstallation(forbidden,forbidden,forbidden)
        invoke=lambda:adapter.execute_shown_removal_task(ctx,artifacts,installation)
    with pytest.raises(TaskCancelled if loss=='cancel' else AdoptionInvalid if boundary=='installer' else OperationFailure):invoke()
    assert reads==['Params']

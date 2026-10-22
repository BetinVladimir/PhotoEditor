"""Both Apply-copy boundaries fence discovery of verified index dependencies."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from photo_worker.operations.semantic import preview_retouch_adopt_adapter as adapter
from photo_worker.operations.semantic import preview_retouch_adopt_installation as installer
from photo_worker.runtime.executor import OperationFailure, TaskCancelled
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_shown_retouch_task_fixture import fixture


@pytest.mark.parametrize('boundary', ['installer','adapter'])
@pytest.mark.parametrize('loss', ['cancel','fence','storage'])
def test_copy_index_discovery_loss_stops_before_qualification_or_staging(monkeypatch,boundary,loss):
    params=ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(fixture()));raw=canonical_bytes(params)
    refs=(params.approved_edit,params.presentation,params.numerical_evidence,params.working_image,params.pixels)
    expected=dict(model_lock_sha=params.model_lock_sha,runtime_lock_sha=params.runtime_lock_sha,
        device_profile_sha=params.device_profile_sha,dataset_sha='d'*64)
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    root=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu',
        tenant_id=params.tenant_id,run_id=params.child_run_id,params=root,inputs=[root]),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',qualification={},resource_estimate=estimate),fence=1))
    cancelled=False;events=[]
    def active():
        if cancelled:raise TaskCancelled('cancelled during index read')
    ctx.check_active=active
    def forbidden(*args,**kwargs):raise AssertionError('authority loss reached external processing')
    ctx.progress=forbidden
    def discover(*,maximum):
        nonlocal cancelled
        assert maximum==512;events.append('index')
        if loss=='cancel':cancelled=True
        elif loss=='fence':ctx.lease.fence+=1
        else:raise OSError('index storage unavailable')
        return refs
    artifacts=SimpleNamespace(contract_bytes=lambda *args:raw,declared_input_refs=discover)
    monkeypatch.setattr(installer,'ShownRetouchQualificationPin',SimpleNamespace(from_value=lambda value:SimpleNamespace(**expected)))
    monkeypatch.setattr(installer,'local_exposure_icc_verifier',lambda registry:forbidden)
    monkeypatch.setattr(installer,'_qualification_loader',forbidden)
    monkeypatch.setattr(adapter,'execute_shown_retouch_adoption',forbidden)
    if boundary=='installer':
        execute=installer.sql_shown_retouch_factory(database=SimpleNamespace(
            admit_semantic_shown_retouch=forbidden,shown_retouch_report_evidence=forbidden),
            objects=SimpleNamespace(get_object=forbidden),bucket='photos',icc_registry=object(),**expected)
        invoke=lambda:execute(ctx,artifacts)
    else:
        installation=adapter.ShownRetouchInstallation(forbidden,forbidden,forbidden)
        invoke=lambda:adapter.execute_shown_retouch_task(ctx,artifacts,installation)
    expected_error=TaskCancelled if loss=='cancel' else OSError if loss=='storage' else AdoptionInvalid if boundary=='installer' else OperationFailure
    with pytest.raises(expected_error):invoke()
    assert events==['index']

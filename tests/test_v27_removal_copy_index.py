"""Apply-copy uses frozen full index, independent from transport-only roots."""
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRemovalAdoptionInput
from photo_worker.operations.semantic import preview_removal_adopt_adapter as adapter
from photo_worker.operations.semantic import preview_removal_adopt_installation as installer
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_removal_preview_adopt_execute import fixture


@pytest.mark.parametrize('boundary',['installer','adapter'])
@pytest.mark.parametrize('failure',[None,'missing','sha','duplicate','no_index','late_fence'])
def test_copy_membership_comes_from_full_verified_index(monkeypatch,boundary,failure):
    params=ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(fixture()));raw=canonical_bytes(params)
    refs=[ref.model_copy(deep=True) for ref in (params.approved_edit,params.presentation,params.numerical_evidence,params.working_image,params.pixels)]
    if failure=='missing':refs.pop()
    if failure=='sha':refs[0].sha256='e'*64
    if failure=='duplicate':refs.append(refs[0])
    root=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    expected=dict(model_lock_sha=params.model_lock_sha,runtime_lock_sha=params.runtime_lock_sha,
        device_profile_sha=params.device_profile_sha,dataset_sha='d'*64)
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='semantic.adopt_shown_removal',operation_version='1',resource_class='cpu',
        tenant_id=params.tenant_id,run_id=params.child_run_id,params=root,inputs=[root]),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',qualification={},resource_estimate=estimate),fence=1),check_active=lambda:None,progress=lambda *args:None)
    calls=[]
    def discover(*,maximum):
        assert maximum==512
        if failure=='late_fence':ctx.lease.fence+=1
        return tuple(refs)
    artifacts=SimpleNamespace(contract_bytes=lambda *args:raw)
    if failure!='no_index':artifacts.declared_input_refs=discover
    def admission(*args):calls.append('admission');return True
    def report(*args,**kwargs):calls.append('report');return lambda:None
    monkeypatch.setattr(installer,'ShownRemovalQualificationPin',SimpleNamespace(from_value=lambda value:SimpleNamespace(**expected)))
    monkeypatch.setattr(installer,'local_exposure_icc_verifier',lambda registry:lambda *args:True)
    monkeypatch.setattr(installer,'_qualification_loader',report)
    def stage(*args,**kwargs):
        calls.append('stage');return SimpleNamespace(image=root,bytes_written=7)
    monkeypatch.setattr(adapter,'execute_shown_removal_adoption',stage)
    if boundary=='installer':
        execute=installer.sql_shown_removal_factory(database=SimpleNamespace(
            admit_semantic_shown_removal=admission,shown_removal_report_evidence=lambda *args:None),
            objects=SimpleNamespace(get_object=lambda *args:None),bucket='photos',icc_registry=object(),**expected)
        invoke=lambda:execute(ctx,artifacts)
    else:invoke=lambda:adapter.execute_shown_removal_task(ctx,artifacts,adapter.ShownRemovalInstallation(admission,lambda *args:True,lambda *args:True))
    if failure is None:
        result=invoke()
        assert calls
        if boundary=='adapter':assert result.outputs==[root] and calls==['stage']
    else:
        from photo_worker.runtime.executor import OperationFailure
        with pytest.raises((ValueError,AdoptionInvalid,OperationFailure)):invoke()
        assert calls==[]

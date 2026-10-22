"""Preview dependency binding with transport-only commands; no model execution."""
from contextlib import contextmanager
from hashlib import sha256
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
from photo_worker.operations.semantic import preview_removal_adapter as module
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.runtime.executor import OperationFailure,TaskCancelled
from test_v27_removal_preview_binding import removal_fixture


@pytest.mark.parametrize('failure',[None,'missing','sha','no_index','params_cancel','index_lease','final_progress','source_cancel','source_lease'])
def test_preview_uses_full_index_and_rechecks_activity(monkeypatch,tmp_path,failure):
    from photo_worker.operations.semantic import preview_removal_complete
    from photo_worker.v2 import removal_qualification_pin
    value,_,_=removal_fixture();params=SemanticRemovalPreviewInput.model_validate_json(canonical_bytes(value));raw=canonical_bytes(params)
    root=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())
    refs=[ref.model_copy(deep=True) for ref in (params.plan,params.intent,params.candidate,params.source.source,params.protection)]
    if failure=='missing':refs.pop()
    if failure=='sha':refs[0].sha256='e'*64
    profile=dict(backend='cuda',renderer_version='v2.2',qualification={},resource_estimate=dict(ram_bytes=2**31,
        scratch_bytes=2**31,vram_bytes=2**30,output_bytes=2**30))
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(operation='semantic.preview_remove_object',
        operation_version='1',resource_class='gpu',tenant_id=params.tenant_id,params=root,inputs=[root]),execution_profile=profile,fence=1))
    cancelled=False;events=[]
    def active():
        if cancelled:raise TaskCancelled('cancelled during Params read')
    ctx.check_active=active
    def progress(done,total):
        if done==1 and failure=='final_progress':ctx.lease.fence+=1
    ctx.progress=progress
    def read(ref,model):
        nonlocal cancelled
        if ref==root:
            if failure=='params_cancel':cancelled=True
            return raw
        events.append('source')
        if failure=='source_cancel':cancelled=True
        if failure=='source_lease':ctx.lease.fence+=1
        return b'{}'
    def discover(*,maximum):
        assert maximum==512
        if failure=='index_lease':ctx.lease.fence+=1
        return tuple(refs)
    artifacts=SimpleNamespace(contract_bytes=read)
    if failure!='no_index':artifacts.declared_input_refs=discover
    pin=SimpleNamespace(backend='cuda',model_lock_sha=params.model_lock_sha,
        runtime_lock_sha=params.runtime_lock_sha,device_profile_sha=params.device_profile_sha)
    monkeypatch.setattr(removal_qualification_pin.RemovalQualificationPin,'from_value',lambda value:pin)
    monkeypatch.setattr(module,'bind_removal_preview_source',lambda *args:SimpleNamespace(canvas=SimpleNamespace(width=8,height=8)))
    output=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    def complete(*args,**kwargs):events.append('compute');return SimpleNamespace(presentation=output,bytes_written=7)
    monkeypatch.setattr(preview_removal_complete,'execute_complete_removal_preview',complete)
    @contextmanager
    def acquire(*args):
        events.append('acquire')
        try:yield object()
        finally:events.append('release')
    pool=SimpleNamespace(lock=SimpleNamespace(lock_id=params.model_lock_sha),acquire=acquire)
    installation=module.RemovalPreviewInstallation(*(lambda *args:True for _ in range(4)),
        presentation=PreviewPresentationPins(tmp_path,'a'*64,'b'*64))
    def run():return module.execute_removal_preview_task(ctx,artifacts,installation,pool,'cuda')
    if failure is None:
        result=run();assert result.outputs==[output]
    else:
        with pytest.raises((ValueError,OperationFailure,TaskCancelled)):run()
    assert events==(['source','acquire','compute','release'] if failure in (None,'final_progress') else ['source'] if failure in ('source_cancel','source_lease') else [])

from photo_worker.contracts.base import canonical_bytes
from types import SimpleNamespace
import pytest
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.preview_retouch_adopt_adapter import (ShownRetouchInstallation,
    execute_shown_retouch_task,shown_retouch_operations)


def test_copy_adapter_requires_explicit_installer():
    with pytest.raises(ValueError):shown_retouch_operations(None,installation_factory=None)


@pytest.mark.parametrize('operation,backend',[('semantic.retouch','cpu'),('semantic.adopt_shown_retouch_region','cuda')])
def test_copy_adapter_rejects_rerender_or_wrong_backend_before_io(operation,backend):
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        command=SimpleNamespace(operation=operation,operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend=backend,renderer_version='v2.2')))
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('invalid task IO')
    with pytest.raises(OperationFailure):
        execute_shown_retouch_task(ctx,Forbidden(),ShownRetouchInstallation(*(lambda *args:True for _ in range(3))))


@pytest.mark.parametrize('field,value',[
    ('ram_bytes',67108864),('scratch_bytes',24*256+32768+16777216-1),
    ('output_bytes',12*256+16384+1048576-1),('vram_bytes',1),
    ('pixel_count',True),('ram_bytes',True),('unexpected',0),
])
def test_copy_adapter_resource_refusal_precedes_any_artifact_read(field,value):
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    estimate[field]=value
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        command=SimpleNamespace(operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate)))
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('invalid budget reached object IO')
    with pytest.raises(OperationFailure):
        execute_shown_retouch_task(ctx,Forbidden(),ShownRetouchInstallation(*(lambda *args:True for _ in range(3))))


@pytest.mark.parametrize('rotate',[False,True])
@pytest.mark.parametrize('transport_only',[False,True])
def test_copy_adapter_rechecks_lease_after_final_progress(monkeypatch,rotate,transport_only):
    from copy import deepcopy
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
    from photo_worker.operations.semantic import preview_retouch_adopt_adapter as module
    from test_v27_shown_retouch_task_fixture import fixture
    params=ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(fixture()))
    raw=canonical_bytes(params)
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    refs=[params.approved_edit,params.presentation,params.numerical_evidence,params.working_image,params.pixels]
    lease=SimpleNamespace(command=SimpleNamespace(operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu',
        tenant_id=params.tenant_id,run_id=params.child_run_id,inputs=refs,
        params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate),fencing_token=1)
    if transport_only:lease.command.inputs=[lease.command.params]
    progress=[]
    ctx=SimpleNamespace(lease=lease,check_active=lambda:None)
    def report(done,total):
        progress.append((done,total))
        if done==1 and rotate:
            ctx.lease=deepcopy(ctx.lease)
            ctx.lease.fencing_token+=1
    ctx.progress=report
    image=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    # Only the adapter boundary is exercised here; physical staging has separate tests.
    monkeypatch.setattr(module,'execute_shown_retouch_adoption',lambda *args,**kwargs:SimpleNamespace(image=image,bytes_written=123))
    artifacts=SimpleNamespace(contract_bytes=lambda *args:raw,declared_input_refs=lambda **kwargs:tuple(refs))
    installation=ShownRetouchInstallation(*(lambda *args:True for _ in range(3)))
    if rotate:
        with pytest.raises(OperationFailure):execute_shown_retouch_task(ctx,artifacts,installation)
    else:
        result=execute_shown_retouch_task(ctx,artifacts,installation)
        assert result.outputs==[image] and result.bytes_written==123 and result.peak_device_bytes==0
    assert progress==[(0,1),(1,1)]


@pytest.mark.parametrize('loss', ['cancel', 'lease'])
def test_shown_retouch_adapter_stops_before_parsing_after_params_scope_loss(loss):
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    cancelled=False
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu',params=None),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate),fencing_token=1))
    def active():
        if cancelled:
            raise InterruptedError('cancelled during copy params IO')
    ctx.check_active=active
    class Artifacts:
        def contract_bytes(self,ref,model):
            nonlocal cancelled
            if loss=='cancel':cancelled=True
            else:ctx.lease.fencing_token+=1
            return b'{}'
    def forbidden(*args):
        raise AssertionError('authority called after scope loss')
    installation=ShownRetouchInstallation(forbidden,forbidden,forbidden)
    with pytest.raises(InterruptedError if loss=='cancel' else OperationFailure):
        execute_shown_retouch_task(ctx,Artifacts(),installation)


@pytest.mark.parametrize('phase',['admission','runtime'])
@pytest.mark.parametrize('loss',['cancel','fence'])
def test_retouch_adapter_rechecks_activity_after_authority_callback(monkeypatch,phase,loss):
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
    from photo_worker.operations.semantic import preview_retouch_adopt_adapter as module
    from test_v27_shown_retouch_task_fixture import fixture
    params=ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(fixture()));raw=canonical_bytes(params)
    estimate=dict(ram_bytes=67108865,vram_bytes=0,scratch_bytes=24*256+32768+16777216,
        output_bytes=12*256+16384+1048576,pixel_count=256)
    ctx=SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='semantic.adopt_shown_retouch_region',operation_version='1',resource_class='cpu',
        tenant_id=params.tenant_id,run_id=params.child_run_id,
        inputs=[params.approved_edit,params.presentation,params.numerical_evidence,params.working_image,params.pixels],
        params=ArtifactRef(artifact_id=uuid4(),sha256=sha256(raw).hexdigest())),
        execution_profile=dict(backend='cpu',renderer_version='v2.2',resource_estimate=estimate),fencing_token=1))
    cancelled=False;progress=[];callbacks=[]
    def active():
        if cancelled:raise InterruptedError('cancelled during authority callback')
    ctx.check_active=active;ctx.progress=lambda done,total:progress.append((done,total))
    def revoke(*args):
        nonlocal cancelled
        callbacks.append(phase)
        if loss=='cancel':cancelled=True
        else:ctx.lease.fencing_token+=1
        return True
    installation=ShownRetouchInstallation(revoke if phase=='admission' else lambda *args:True,
        revoke if phase=='runtime' else lambda *args:True,lambda *args:True)
    # Exercise adapter-owned callback guards only; real NPY staging is covered
    # by test_v27_retouch_preview_adopt_execute rather than this boundary stub.
    def execute(artifacts,value,**kwargs):
        if phase=='admission':kwargs['verify_admission'](value)
        else:kwargs['verify_runtime']()
        raise AssertionError('staging reached after authority callback lost activity')
    monkeypatch.setattr(module,'execute_shown_retouch_adoption',execute)
    with pytest.raises(InterruptedError if loss=='cancel' else OperationFailure):
        execute_shown_retouch_task(ctx,SimpleNamespace(contract_bytes=lambda *args:raw,declared_input_refs=lambda **kwargs:tuple(ctx.lease.command.inputs)),installation)
    assert callbacks==[phase] and progress==[(0,1)]

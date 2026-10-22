from types import SimpleNamespace
import pytest
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.preview_removal_adopt_adapter import (ShownRemovalInstallation,
    execute_shown_removal_task,shown_removal_operations)


def test_copy_adapter_requires_explicit_installer():
    with pytest.raises(ValueError):shown_removal_operations(None,installation_factory=None)


@pytest.mark.parametrize('operation,backend',[('semantic.remove_object','cpu'),('semantic.adopt_shown_removal','cuda')])
def test_copy_adapter_rejects_rerender_or_wrong_backend_before_io(operation,backend):
    ctx=SimpleNamespace(check_active=lambda:None,lease=SimpleNamespace(
        command=SimpleNamespace(operation=operation,operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend=backend,renderer_version='v2.2')))
    class Forbidden:
        def contract_bytes(self,*args):raise AssertionError('invalid task IO')
    with pytest.raises(OperationFailure):
        execute_shown_removal_task(ctx,Forbidden(),ShownRemovalInstallation(*(lambda *args:True for _ in range(3))))


@pytest.mark.parametrize('field,value', [('operation', 'semantic.remove_object'),
    ('operation_version', '2'), ('resource_class', 'gpu'),
    ('backend', 'mps'), ('renderer_version', 'v2.1')])
def test_registered_copy_route_rejects_before_artifacts_and_installer(monkeypatch, field, value):
    from photo_worker.operations.semantic import preview_removal_adopt_adapter as module
    command = SimpleNamespace(operation='semantic.adopt_shown_removal',
        operation_version='1', resource_class='cpu')
    profile = dict(backend='cpu', renderer_version='v2.2')
    if field in profile:profile[field] = value
    else:setattr(command, field, value)
    ctx = SimpleNamespace(check_active=lambda: None,
        lease=SimpleNamespace(command=command, execution_profile=profile))
    def forbidden(*args, **kwargs):
        raise AssertionError('invalid route reached artifact or installer IO')
    monkeypatch.setattr(module.TaskArtifacts, 'create', forbidden)
    operation = shown_removal_operations(None, installation_factory=forbidden)[0]
    with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
        operation.execute(ctx)


@pytest.mark.parametrize('loss', ['lease', 'cancel'])
def test_copy_final_progress_loss_prevents_success(monkeypatch, loss):
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes
    from photo_worker.contracts.semantic_preview import ShownRemovalAdoptionInput
    from photo_worker.runtime.executor import TaskCancelled
    from photo_worker.operations.semantic import preview_removal_adopt_adapter as module
    from test_v27_removal_preview_adopt_execute import fixture
    params = ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(fixture()))
    raw = canonical_bytes(params)
    root = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    cancelled = False; events = []
    command = SimpleNamespace(operation='semantic.adopt_shown_removal', operation_version='1',
        resource_class='cpu', tenant_id=params.tenant_id, run_id=params.child_run_id, params=root)
    profile = dict(backend='cpu', renderer_version='v2.2', resource_estimate=dict(
        ram_bytes=67108864, vram_bytes=0, scratch_bytes=24*2+32768+16777216,
        output_bytes=12*2+16384+1048576, pixel_count=2))
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command, execution_profile=profile, fence=1))
    def active():
        if cancelled:raise TaskCancelled('cancelled at final progress')
    def progress(done, total):
        nonlocal cancelled
        events.append(('progress', done))
        if done:
            if loss == 'lease':ctx.lease.fence += 1
            else:cancelled = True
    ctx.check_active = active; ctx.progress = progress
    artifacts = SimpleNamespace(contract_bytes=lambda *args: raw,
        declared_input_refs=lambda **kwargs: tuple(getattr(params, key) for key in
            ('approved_edit', 'presentation', 'numerical_evidence', 'working_image', 'pixels')))
    def copy(*args, **kwargs):
        events.append(('copy', 1))
        return SimpleNamespace(image=root, bytes_written=7)
    monkeypatch.setattr(module, 'execute_shown_removal_adoption', copy)
    installation = ShownRemovalInstallation(*(lambda *args: True for _ in range(3)))
    with pytest.raises(TaskCancelled if loss == 'cancel' else OperationFailure):
        execute_shown_removal_task(ctx, artifacts, installation)
    assert events == [('progress', 0), ('copy', 1), ('progress', 1)]


@pytest.mark.parametrize('field,value', [(None, None),
    ('ram_bytes', 67108863), ('scratch_bytes', 24*2+32768+16777216-1),
    ('output_bytes', 12*2+16384+1048576-1), ('vram_bytes', 1),
    ('pixel_count', 0), ('pixel_count', 200000001),
    *[(key, value) for key in ('ram_bytes', 'scratch_bytes', 'output_bytes', 'vram_bytes', 'pixel_count')
        for value in (True, -1, 2**53)]])
def test_copy_resource_boundary_precedes_all_document_io(monkeypatch, field, value):
    from hashlib import sha256
    from uuid import uuid4
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes
    from photo_worker.contracts.semantic_preview import ShownRemovalAdoptionInput
    from photo_worker.operations.semantic import preview_removal_adopt_adapter as module
    from test_v27_removal_preview_adopt_execute import fixture
    params = ShownRemovalAdoptionInput.model_validate_json(canonical_bytes(fixture())); raw = canonical_bytes(params)
    root = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
    estimate = dict(ram_bytes=67108864, vram_bytes=0, scratch_bytes=24*2+32768+16777216,
        output_bytes=12*2+16384+1048576, pixel_count=2)
    if field is not None:estimate[field] = value
    calls = []
    command = SimpleNamespace(operation='semantic.adopt_shown_removal', operation_version='1',
        resource_class='cpu', tenant_id=params.tenant_id, run_id=params.child_run_id, params=root)
    ctx = SimpleNamespace(check_active=lambda: None, progress=lambda *args: None,
        lease=SimpleNamespace(command=command,
            execution_profile=dict(backend='cpu', renderer_version='v2.2', resource_estimate=estimate)))
    class Artifacts:
        def contract_bytes(self, *args):
            calls.append('params'); return raw
        def declared_input_refs(self, **kwargs):
            calls.append('index')
            return tuple(getattr(params, key) for key in
                ('approved_edit', 'presentation', 'numerical_evidence', 'working_image', 'pixels'))
    def copy(*args, **kwargs):
        calls.append('copy')
        assert kwargs['expected_pixel_count'] == 2
        assert kwargs['max_output_bytes'] == estimate['output_bytes']
        return SimpleNamespace(image=root, bytes_written=7)
    monkeypatch.setattr(module, 'execute_shown_removal_adoption', copy)
    installation = ShownRemovalInstallation(*(lambda *args: True for _ in range(3)))
    if field is None:
        result = execute_shown_removal_task(ctx, Artifacts(), installation)
        assert result.outputs == [root] and result.bytes_written == 7 and result.peak_device_bytes == 0
        assert calls == ['params', 'index', 'copy']
    else:
        with pytest.raises(OperationFailure, match='CONFIG_INVALID'):
            execute_shown_removal_task(ctx, Artifacts(), installation)
        assert calls == []

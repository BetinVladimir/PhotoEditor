"""Registered wrapper ownership and error semantics; no task execution IO."""
from contextlib import contextmanager
from types import SimpleNamespace
import pytest
from photo_worker.operations.semantic import preview_retouch_adapter as preview
from photo_worker.operations.semantic import preview_retouch_adopt_adapter as shown
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.runtime.executor import OperationFailure, OperationResult, TaskCancelled, LeaseLost
from photo_worker.v2.adoption import AdoptionInvalid


@pytest.mark.parametrize('kind', ['preview','shown'])
@pytest.mark.parametrize('phase', ['installation','execution'])
@pytest.mark.parametrize('failure', ['invalid','adoption','cancel','lease','timeout','storage',None])
def test_registered_operation_owns_artifacts_and_preserves_failure(monkeypatch,tmp_path,kind,phase,failure):
    module = preview if kind == 'preview' else shown
    name = 'semantic.preview_retouch_region' if kind == 'preview' else 'semantic.adopt_shown_retouch_region'
    ctx = SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation=name,operation_version='1',resource_class='cpu'),
        execution_profile=dict(backend='cpu',renderer_version='v2.2')))
    errors = {'invalid':ValueError('invalid'),'adoption':AdoptionInvalid('invalid'),
        'cancel':TaskCancelled('cancel'),'lease':LeaseLost('lost'),
        'timeout':OperationFailure('TIMEOUT',True),'storage':OSError('storage unavailable')}
    error = errors.get(failure)
    events = []
    artifacts, installation = object(), object()
    expected = OperationResult(outputs=[],bytes_written=7,peak_device_bytes=0)
    @contextmanager
    def create(owner, config, roles):
        assert owner is ctx and config == 'configured'
        assert roles == (preview.ROLES if kind == 'preview' else frozenset({'pixels','working_image'}))
        events.append('open')
        try:yield artifacts
        finally:events.append('close')
    def install(owner, supplied):
        assert owner is ctx and supplied is artifacts
        events.append('installation')
        if phase == 'installation' and error is not None:raise error
        return installation
    def execute(owner,supplied,installed,*args):
        assert owner is ctx and supplied is artifacts and installed is installation
        events.append('execution')
        if phase == 'execution' and error is not None:raise error
        return expected
    monkeypatch.setattr(module,'TaskArtifacts',SimpleNamespace(create=create))
    monkeypatch.setattr(module,'execute_retouch_preview_task' if kind == 'preview' else 'execute_shown_retouch_task',execute)
    if kind == 'preview':
        operation=preview.retouch_preview_operations('configured',installation_factory=install,
            presentation=PreviewPresentationPins(tmp_path,'a'*64,'b'*64))[0]
    else:operation=shown.shown_retouch_operations('configured',installation_factory=install)[0]
    if error is None:
        assert operation.execute(ctx) is expected
    else:
        with pytest.raises(OperationFailure if failure in ('invalid','adoption') else type(error)) as caught:
            operation.execute(ctx)
        if failure in ('invalid','adoption'):
            assert caught.value.error.code == 'TASK_INPUTS_INVALID'
            assert caught.value.error.retryable is False
        else:
            assert caught.value is error
    assert events == (['open','installation','close'] if error is not None and phase == 'installation'
        else ['open','installation','execution','close'])

"""Own CPU operation wiring with synthetic SQL/report and publication doubles."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.operations.semantic import preview_retouch_adapter as module
from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
from photo_worker.v2.adoption import AdoptionInvalid
from test_v27_retouch_preview_installation import fixture


@pytest.mark.parametrize('failure',[None,'late_lease','resource'])
def test_retouch_adapter_returns_only_verified_presentation(monkeypatch,tmp_path,failure):
    ctx,artifacts,options,calls,_,params,_,_ = fixture(monkeypatch)
    estimate = ctx.lease.execution_profile['resource_estimate']
    estimate.update(scratch_bytes=1 << 30,output_bytes=32*1048576)
    ctx.progress = lambda current,total:calls.append(('progress',current,total))
    installation = module_install(options,ctx,artifacts)
    pins = PreviewPresentationPins(tmp_path,'a'*64,'b'*64)
    root = ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    def complete(supplied_artifacts,supplied_params,execute,**kwargs):
        assert supplied_artifacts is artifacts and supplied_params == params
        assert execute is installation
        assert kwargs['declared_inputs'] == ctx.lease.command.inputs
        assert kwargs['verify_runtime']() is True
        assert kwargs['verify_admission'](supplied_params) is True
        assert kwargs['max_ram_bytes'] == estimate['ram_bytes']
        if failure == 'late_lease':ctx.lease.fence += 1
        return SimpleNamespace(presentation=root,bytes_written=1234)
    monkeypatch.setattr(module,'execute_complete_retouch_preview',complete)
    if failure == 'resource':
        # Profile changed before installation; malformed budget is rejected by adapter.
        estimate['output_bytes'] = True
        ctx.lease.execution_profile['resource_estimate']['output_bytes'] = True
        # Reinstall to freeze this profile while keeping SQL admission synthetic.
        installation = module_install(options,ctx,artifacts)
    if failure:
        with pytest.raises((AdoptionInvalid,module.OperationFailure)):
            module.execute_retouch_preview_task(ctx,artifacts,installation,pins)
    else:
        result = module.execute_retouch_preview_task(ctx,artifacts,installation,pins)
        assert result.outputs == [root] and result.peak_device_bytes == 0
        assert result.bytes_written == 1234 and result.review_required is False
        assert calls[-1] == ('progress',1,1)


def module_install(options,ctx,artifacts):
    from photo_worker.operations.semantic.preview_retouch_installation import sql_retouch_preview_factory
    return sql_retouch_preview_factory(**options)(ctx,artifacts)


def test_retouch_operation_is_explicit_cpu_only(tmp_path):
    pins = PreviewPresentationPins(tmp_path,'a'*64,'b'*64)
    operations = module.retouch_preview_operations(None,installation_factory=lambda *a:None,presentation=pins)
    assert len(operations) == 1
    assert (operations[0].name,operations[0].resource_class,operations[0].version) == ('semantic.preview_retouch_region','cpu','1')
    with pytest.raises(ValueError):module.retouch_preview_operations(None,installation_factory=None,presentation=pins)
    with pytest.raises(ValueError):module.retouch_preview_operations(None,installation_factory=lambda *a:None,presentation=None)


@pytest.mark.parametrize('phase', ['installation', 'execution'])
@pytest.mark.parametrize('failure', ['value', 'adoption', 'cancel', 'lease', 'timeout', 'storage'])
def test_registered_retouch_operation_closes_artifacts_and_preserves_failure_kind(monkeypatch, tmp_path, phase, failure):
    from contextlib import contextmanager
    from photo_worker.runtime.executor import TaskCancelled, LeaseLost, OperationFailure
    ctx = SimpleNamespace(lease=SimpleNamespace(command=SimpleNamespace(
        operation='semantic.preview_retouch_region', operation_version='1', resource_class='cpu'),
        execution_profile=dict(backend='cpu', renderer_version='v2.2')))
    artifacts = object()
    installation = object()
    events = []
    errors = dict(value=ValueError('invalid'), adoption=AdoptionInvalid('invalid'),
        cancel=TaskCancelled('cancelled'), lease=LeaseLost('lost'),
        timeout=OperationFailure('TIMEOUT', True), storage=OSError('storage unavailable'))
    error = errors[failure]
    @contextmanager
    def create(owner, config, roles):
        assert owner is ctx and config == 'configured' and roles == module.ROLES
        events.append('open')
        try: yield artifacts
        finally: events.append('close')
    def install(owner, supplied):
        assert owner is ctx and supplied is artifacts
        events.append('installation')
        if phase == 'installation': raise error
        return installation
    def execute(owner, supplied, installed, presentation):
        assert owner is ctx and supplied is artifacts and installed is installation
        events.append('execution')
        raise error
    monkeypatch.setattr(module, 'TaskArtifacts', SimpleNamespace(create=create))
    monkeypatch.setattr(module, 'execute_retouch_preview_task', execute)
    pins = PreviewPresentationPins(tmp_path, 'a' * 64, 'b' * 64)
    operation = module.retouch_preview_operations('configured', installation_factory=install, presentation=pins)[0]
    with pytest.raises(OperationFailure if failure in ('value', 'adoption') else type(error)) as caught:
        operation.execute(ctx)
    if failure in ('value', 'adoption'):
        assert caught.value.error.code == 'TASK_INPUTS_INVALID'
    else:
        assert caught.value is error
    assert events == (['open', 'installation', 'close'] if phase == 'installation'
        else ['open', 'installation', 'execution', 'close'])

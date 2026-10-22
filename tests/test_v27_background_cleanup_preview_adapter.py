import pytest
from types import SimpleNamespace
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic.preview_adapter import ExposurePreviewInstallation
from photo_worker.operations.semantic.preview_background_cleanup_adapter import (
    background_cleanup_preview_operations, execute_background_cleanup_preview_task,
)


def test_exposure_installer_cannot_admit_background_cleanup():
    installation=ExposurePreviewInstallation(lambda *a:True,lambda *a:True,
        lambda *a:True,lambda *a:True)
    with pytest.raises(OperationFailure):
        execute_background_cleanup_preview_task(None,None,installation,None,'cuda')


@pytest.mark.parametrize('backend', ['cuda','mps','cpu'])
def test_explicit_backend_registration_requires_installer_and_pool(backend):
    with pytest.raises(ValueError):
        background_cleanup_preview_operations(None,installation_factory=None,pool=None,backend=backend)
    with pytest.raises(ValueError):
        background_cleanup_preview_operations(None,installation_factory=lambda *a:None,pool=None,backend=backend)
    operations=background_cleanup_preview_operations(None,installation_factory=lambda *a:None,
        pool=SimpleNamespace(acquire=lambda *a:None),backend=backend)
    assert len(operations)==1


def test_removal_installer_cannot_admit_background_cleanup():
    from photo_worker.operations.semantic.preview_removal_adapter import RemovalPreviewInstallation
    installation=RemovalPreviewInstallation(lambda *a:True,lambda *a:True,
        lambda *a:True,lambda *a:True)
    with pytest.raises(OperationFailure):
        execute_background_cleanup_preview_task(None,None,installation,None,'mps')


@pytest.mark.parametrize('loss', ['cancel', 'lease'])
def test_cleanup_adapter_stops_after_params_read_changes_execution_scope(loss):
    from photo_worker.operations.semantic.preview_background_cleanup_adapter import BackgroundCleanupPreviewInstallation
    cancelled = False
    command = SimpleNamespace(operation='semantic.preview_background_cleanup', operation_version='1',
        resource_class='gpu', params=None)
    profile = dict(backend='cpu', renderer_version='v2.2',
        resource_estimate=dict(ram_bytes=2**30, scratch_bytes=2**30, vram_bytes=0, output_bytes=2**30))
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command, execution_profile=profile))
    def active():
        if cancelled:
            raise InterruptedError('cancelled during params read')
    ctx.check_active = active
    class Artifacts:
        def contract_bytes(self, ref, model):
            nonlocal cancelled
            if loss == 'cancel':
                cancelled = True
            else:
                ctx.lease.execution_profile['renderer_version'] = 'changed'
            # Deliberately invalid: cancellation/fencing must precede parsing.
            return b'{}'
    def forbidden(*a):
        raise AssertionError('authority invoked after execution scope loss')
    installation = BackgroundCleanupPreviewInstallation(forbidden, forbidden, forbidden, forbidden)
    with pytest.raises(InterruptedError if loss == 'cancel' else OperationFailure):
        execute_background_cleanup_preview_task(ctx, Artifacts(), installation, None, 'cpu')


@pytest.mark.parametrize('backend', ['cpu', 'cuda', 'mps'])
@pytest.mark.parametrize('loss', [None, 'cancel', 'lease', 'admission', 'runtime', 'ram', 'output', 'scratch'])
def test_cleanup_model_acquisition_rechecks_authority_and_releases_engine(tmp_path, monkeypatch, backend, loss):
    from contextlib import contextmanager
    from hashlib import sha256
    from uuid import uuid4, uuid5
    from photo_worker.contracts.base import ArtifactRef, canonical_bytes
    from photo_worker.contracts.semantic_preview import SemanticBackgroundCleanupPreviewInput
    from photo_worker.operations.semantic.preview_adapter import PreviewPresentationPins
    from photo_worker.operations.semantic.preview_background_cleanup_adapter import BackgroundCleanupPreviewInstallation
    from test_v27_background_cleanup_preview import background_fixture
    from test_v27_exposure_source import source_fixture
    params, _, _ = background_fixture()
    edit, _, source_raw = source_fixture()
    params['source'] = edit['source']
    owned = SemanticBackgroundCleanupPreviewInput.model_validate_json(canonical_bytes(params))
    raw = canonical_bytes(owned)
    run_id = uuid4()
    command = SimpleNamespace(operation='semantic.preview_background_cleanup', operation_version='1',
        resource_class='gpu', params=ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest()),
        tenant_id=owned.tenant_id, run_id=run_id,
        task_id=uuid5(run_id, 'semantic.preview_background_cleanup:'+str(owned.target_id)),
        inputs=[owned.plan, owned.intent, owned.candidate, owned.source.source, owned.protection])
    pin = dict(policy_id=str(uuid4()), qualification_id=str(uuid4()), issuer_id=str(uuid4()),
        policy_sha='a'*64, dataset_sha='b'*64, backend=backend,
        model_lock_sha=owned.model_lock_sha, runtime_lock_sha=owned.runtime_lock_sha,
        device_profile_sha=owned.device_profile_sha,
        report=dict(artifact_id=str(uuid4()), sha256='c'*64),
        calibration=dict(artifact_id=str(uuid4()), sha256='d'*64))
    profile = dict(backend=backend, renderer_version='v2.2', qualification=pin,
        resource_estimate=dict(ram_bytes=2**30, scratch_bytes=2**30,
            vram_bytes=0 if backend == 'cpu' else 10**9, output_bytes=2**30))
    if loss in ('ram', 'output', 'scratch'):
        profile['resource_estimate'][{'ram': 'ram_bytes', 'output': 'output_bytes', 'scratch': 'scratch_bytes'}[loss]] = 1
    events = []
    acquired = False
    ctx = SimpleNamespace(lease=SimpleNamespace(command=command, execution_profile=profile),
        progress=lambda *a: None)
    def active():
        if acquired and loss == 'cancel':
            raise InterruptedError('cancelled during model acquire')
    ctx.check_active = active
    class Artifacts:
        def contract_bytes(self, ref, model):
            return raw if ref == command.params else source_raw
    class Pool:
        lock = SimpleNamespace(lock_id=owned.model_lock_sha)
        @contextmanager
        def acquire(self, model_id, supplied_backend):
            nonlocal acquired
            assert (model_id, supplied_backend) == (owned.model_id, backend)
            events.append('acquire')
            acquired = True
            if loss == 'lease':
                ctx.lease.execution_profile['renderer_version'] = 'changed'
            try:
                yield object()
            finally:
                events.append('release')
    installation = BackgroundCleanupPreviewInstallation(
        lambda *a: not (acquired and loss == 'admission'), lambda *a: True, lambda *a: True,
        lambda *a: not (acquired and loss == 'runtime'),
        PreviewPresentationPins(tmp_path.resolve(), 'e'*64, 'f'*64))
    presentation = ArtifactRef(artifact_id=uuid4(), sha256='a'*64)
    def complete(*args, **kwargs):
        events.append('compute')
        assert loss is None
        return SimpleNamespace(presentation=presentation, bytes_written=123)
    monkeypatch.setattr('photo_worker.operations.semantic.preview_background_cleanup_complete.execute_complete_background_cleanup_preview', complete)
    if loss in ('ram', 'output', 'scratch'):
        with pytest.raises(OperationFailure if loss == 'scratch' else ValueError):
            execute_background_cleanup_preview_task(ctx, Artifacts(), installation, Pool(), backend)
        assert events == []
    elif loss:
        with pytest.raises(InterruptedError if loss == 'cancel' else OperationFailure):
            execute_background_cleanup_preview_task(ctx, Artifacts(), installation, Pool(), backend)
        assert events == ['acquire', 'release']
    else:
        result = execute_background_cleanup_preview_task(ctx, Artifacts(), installation, Pool(), backend)
        assert result.outputs == [presentation] and result.bytes_written == 123
        assert result.peak_device_bytes == (0 if backend == 'cpu' else None)
        assert events == ['acquire', 'compute', 'release']

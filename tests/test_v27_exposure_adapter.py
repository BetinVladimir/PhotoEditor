from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.executor import OperationFailure
from photo_worker.operations.semantic import exposure_adapter as adapter
from test_v27_exposure_admission import setup
from test_v27_exposure_source import source_fixture
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.base import canonical_bytes
from hashlib import sha256


def configured():
    edit, lease, calls, ctx, database = setup()
    source_edit, _, source = source_fixture()
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(source_edit))
    lease.command.params.sha256 = sha256(canonical_bytes(edit)).hexdigest()
    lease.execution_profile['resource_estimate'] = dict(ram_bytes=128*1024**2,
        scratch_bytes=2*1024**2, vram_bytes=0, output_bytes=2*1024**2)
    database.local_exposure_protection_origin = lambda *args: True
    ctx.progress = lambda *args: None
    artifacts = SimpleNamespace(params=lambda model: edit, contract_bytes=lambda ref, model: source)
    installation = adapter.LocalExposureInstallation(database,
        lambda edit: ArtifactRef(artifact_id=uuid4(), sha256='f'*64),
        lambda color: True, lambda edit, profile: True)
    return ctx, artifacts, installation


def test_adapter_returns_only_staged_working_image(monkeypatch):
    ctx, artifacts, installation = configured()
    root = ArtifactRef(artifact_id=uuid4(), sha256='e'*64)
    def execute(artifacts, edit, **kwargs):
        assert kwargs['verify_admission'](edit) is True
        return SimpleNamespace(root=root, review_required=False, bytes_written=123)
    monkeypatch.setattr(adapter, 'execute_and_publish_local_exposure', execute)
    result = adapter.execute_exposure(ctx, artifacts, installation)
    assert result.outputs == [root] and result.bytes_written == 123
    assert result.review_required is False and result.peak_device_bytes == 0


@pytest.mark.parametrize('field,value', [('ram_bytes', True), ('output_bytes', -1), ('scratch_bytes', None)])
def test_invalid_resource_profile_never_computes(monkeypatch, field, value):
    ctx, artifacts, installation = configured()
    ctx.lease.execution_profile['resource_estimate'][field] = value
    monkeypatch.setattr(adapter, 'execute_and_publish_local_exposure', lambda *a, **k: pytest.fail('computed'))
    with pytest.raises(OperationFailure):adapter.execute_exposure(ctx, artifacts, installation)


def test_runtime_denial_never_computes(monkeypatch):
    ctx, artifacts, installed = configured()
    installation = adapter.LocalExposureInstallation(installed.database,
        installed.resolve_protection, installed.verify_color, lambda *args: 1)
    monkeypatch.setattr(adapter, 'execute_and_publish_local_exposure', lambda *a, **k: pytest.fail('computed'))
    with pytest.raises(OperationFailure):adapter.execute_exposure(ctx, artifacts, installation)


def test_missing_installer_denied():
    with pytest.raises(ValueError):adapter.local_exposure_operations(object(), installation_factory=None)


@pytest.mark.parametrize('boundary',['install','execute'])
def test_operation_classifies_qualification_rejection_and_closes_artifacts(monkeypatch,boundary):
    from contextlib import contextmanager
    from photo_worker.v2.adoption import AdoptionInvalid
    events=[];artifacts=object()
    @contextmanager
    def create(*args):
        events.append('open')
        try:yield artifacts
        finally:events.append('close')
    monkeypatch.setattr(adapter.TaskArtifacts,'create',create)
    def reject(*args):raise AdoptionInvalid('private object path and scope')
    factory=reject if boundary=='install' else lambda *args:object()
    monkeypatch.setattr(adapter,'execute_exposure',reject)
    operation=adapter.local_exposure_operations(object(),installation_factory=factory)[0]
    with pytest.raises(OperationFailure) as error:operation.execute(object())
    assert error.value.error.code=='TASK_INPUTS_INVALID'
    assert not error.value.error.retryable and error.value.error.args=={}
    assert 'private' not in str(error.value) and events==['open','close']


def test_operation_preserves_dependency_retry_policy(monkeypatch):
    from contextlib import contextmanager
    @contextmanager
    def create(*args):yield object()
    monkeypatch.setattr(adapter.TaskArtifacts,'create',create)
    failure=OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True)
    def unavailable(*args):raise failure
    operation=adapter.local_exposure_operations(object(),installation_factory=unavailable)[0]
    with pytest.raises(OperationFailure) as error:operation.execute(object())
    assert error.value is failure and error.value.error.retryable

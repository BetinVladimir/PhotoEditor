"""Deferred real build_worker wiring with isolated TLS/model/catalog dependencies."""
from dataclasses import replace
from pathlib import Path
from types import SimpleNamespace as NS
from uuid import uuid4
import ssl
import pytest

from photo_worker.runtime import service


def setup_worker(monkeypatch, tmp_path, backend, status='passed', files_ready=True, sql_ready=True):
    env = dict(PHOTO_WORKER_ID=str(uuid4()), PHOTO_WORKER_CLASS='gpu',
        PHOTO_WORKER_BACKEND=backend, DATABASE_URL='postgres://example.test/photo',
        AMQP_URL='amqps://example.test', PHOTO_SCRATCH_ROOT=str(tmp_path),
        GATEWAY_ORIGIN='https://localhost:8443', GATEWAY_CA_FILE='ca.pem',
        WORKER_CERT_FILE='worker.pem', WORKER_KEY_FILE='worker.key',
        WORKER_MAX_SCRATCH_BYTES='4096', WORKER_MAX_DOWNLOAD_BYTES='4096',
        WORKER_MAX_UPLOAD_BYTES='4096')
    if backend == 'mps':
        env['PHOTO_CAPACITY_POLICY'] = str(Path(__file__).resolve().parents[2] / 'configs/capacity-metal-verification.json')
    config = service.WorkerConfig.from_env(env)
    path = tmp_path / 'models.json'
    path.write_bytes(b'{}')
    config = replace(config, model_root=tmp_path, model_lock=path)
    events = []
    entry = NS(id='big_lama', approved_for_install=True,
        backends={b: NS(status=status, report_sha256='a'*64) for b in ('cpu','cuda','mps')})
    lock = NS(lock_id='b'*64, models=[entry])
    from photo_worker.models import lock as locks, pool as pools, adapters, install
    monkeypatch.setattr(locks.ModelLock, 'model_validate_json', lambda *_: lock)
    monkeypatch.setattr(adapters, 'factories', lambda: {'big_lama': object()})
    monkeypatch.setattr(pools, 'ModelPool', lambda root, lock, factories: NS(
        root=root, lock=lock, factories=factories, acquire=lambda *_: None,
        residency_snapshot=lambda: (), close=lambda: None))
    def verify(root, actual):
        events.append('files')
        assert root == tmp_path / lock.lock_id / 'big_lama' and actual is entry
        if not files_ready: raise install.ModelError('MODEL_HASH_MISMATCH')
        return {}
    monkeypatch.setattr(install, 'verify_model', verify)
    def ready():
        events.append('sql')
        return sql_ready
    database = NS(removal_preview_runtime_ready=ready, media_renderer_policy_ready=lambda:True, task_result_fence_ready=lambda:True)
    monkeypatch.setattr(service, 'TaskDatabase', lambda *_: database)
    monkeypatch.setattr(service, 'client_tls', lambda *_: ssl.create_default_context())
    factory = lambda *_: pytest.fail('build must not execute a leased task installation')
    return config, factory, events


@pytest.mark.parametrize('backend', ['cpu','cuda','mps'])
def test_real_build_checks_files_then_sql_before_registering_removal(monkeypatch, tmp_path, backend):
    config, factory, events = setup_worker(monkeypatch, tmp_path, backend)
    _, transport = service.build_worker(config, removal_preview_installation_factory=factory)
    assert events == ['files','sql']
    operation = transport.executor.operations['semantic.preview_remove_object']
    assert operation.resource_class == 'gpu' and operation.version == '1'


def test_real_metal_build_omits_unsupported_removal_without_files_or_sql(monkeypatch, tmp_path):
    config, factory, events = setup_worker(monkeypatch, tmp_path, 'mps', status='unsupported')
    _, transport = service.build_worker(config, removal_preview_installation_factory=factory)
    assert events == []
    assert 'semantic.preview_remove_object' not in transport.executor.operations
    assert 'pair.fit.color' in transport.executor.operations


@pytest.mark.parametrize('failure', ['files','sql'])
def test_real_build_refuses_registration_on_model_or_sql_failure(monkeypatch, tmp_path, failure):
    config, factory, events = setup_worker(monkeypatch, tmp_path, 'cuda',
        files_ready=failure != 'files', sql_ready=failure != 'sql')
    with pytest.raises(service.WorkerConfigurationError):
        service.build_worker(config, removal_preview_installation_factory=factory)
    assert events == (['files'] if failure == 'files' else ['files','sql'])


def test_removal_deployment_policies_keep_cuda_ceiling_and_uncapped_metal(tmp_path,monkeypatch):
    from photo_worker.runtime.capacity_policy import CapacityPolicy
    from photo_worker.runtime.capacity import Capacity
    root = Path(__file__).resolve().parents[2] / 'configs'
    cuda = CapacityPolicy.model_validate_json((root / 'capacity-rtx3060.json').read_bytes())
    metal = CapacityPolicy.model_validate_json((root / 'capacity-metal-verification.json').read_bytes())
    assert cuda.gpu_budget_bytes <= 10 * 1024**3
    assert metal.gpu_budget_bytes == 2**53 - 1
    measured = Capacity(24 * 1024**3, 1024, 12 * 1024**3)
    config = NS(capacity_policy=cuda, resource_class='gpu', backend='cuda', scratch_root=tmp_path)
    monkeypatch.setattr('photo_worker.runtime.cuda_budget.remaining_cuda_budget',lambda limit:limit)
    capped = service._capacity_sample(config, measured)
    assert capped.vram_bytes == cuda.gpu_budget_bytes
    assert capped.ram_bytes <= measured.ram_bytes

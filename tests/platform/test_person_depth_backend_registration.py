"""Registration policy scenarios; no model inference or device initialization."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime.service import _model_backend_enabled


@pytest.mark.parametrize('model_id', ['lraspp_mobilenet_v3_large', 'depth_anything_v2_small'])
@pytest.mark.parametrize('backend,status,enabled', [
    ('mps', 'unsupported', False),
    ('mps', 'passed', True),
    ('mps', 'untested', True),
    ('cuda', 'passed', True),
    ('cpu', 'passed', True),
])
def test_person_depth_backend_registration(model_id, backend, status, enabled):
    pool = SimpleNamespace(lock=SimpleNamespace(models=[
        SimpleNamespace(id=model_id, backends={backend: SimpleNamespace(status=status)})]))
    assert _model_backend_enabled(pool, model_id, backend) is enabled


def test_missing_metal_model_is_not_enabled():
    pool = SimpleNamespace(lock=SimpleNamespace(models=[]))
    assert _model_backend_enabled(pool, 'depth_anything_v2_small', 'mps') is False


def test_absent_evidence_is_not_silently_classified_as_unsupported():
    pool = SimpleNamespace(lock=SimpleNamespace(models=[
        SimpleNamespace(id='depth_anything_v2_small', backends={})]))
    assert _model_backend_enabled(pool, 'depth_anything_v2_small', 'mps') is True


@pytest.mark.parametrize('model_id,operation,runtime_module,runtime_builder,runtime_field', [
    ('lraspp_mobilenet_v3_large', 'mask.person', 'person_config', 'configured_person_runtime', 'person_runtime_locks'),
    ('depth_anything_v2_small', 'depth.predict', 'depth_config', 'configured_depth_runtime', 'depth_runtime_locks'),
])
@pytest.mark.parametrize('status', ['passed', 'unsupported', 'missing'])
def test_build_worker_advertises_only_supported_metal_analysis(
        monkeypatch, tmp_path, model_id, operation, runtime_module, runtime_builder, runtime_field, status):
    from dataclasses import replace
    from importlib import import_module
    import ssl
    from test_worker_service import environment
    from photo_worker.runtime import service
    from photo_worker.models import lock as lock_module, pool as pool_module, adapters

    config = service.WorkerConfig.from_env({**environment(tmp_path), 'PHOTO_WORKER_CLASS': 'cpu'})
    model_lock_path = tmp_path / 'model-lock.json'
    model_lock_path.write_text('{}')
    config = replace(config, resource_class='gpu', backend='mps', model_root=tmp_path,
                     model_lock=model_lock_path, **{runtime_field: object()})
    entry = SimpleNamespace(id=model_id, approved_for_install=True,
                            backends={'mps': SimpleNamespace(status=status)})
    lock = SimpleNamespace(models=[] if status == 'missing' else [entry], lock_id='a'*64)
    installed = SimpleNamespace(lock=lock, residency_snapshot=lambda: ())
    monkeypatch.setattr(lock_module.ModelLock, 'model_validate_json', lambda *args: lock)
    monkeypatch.setattr(pool_module, 'ModelPool', lambda *args: installed)
    monkeypatch.setattr(adapters, 'factories', lambda: {})
    monkeypatch.setattr(service, 'client_tls', lambda *args: ssl.create_default_context())
    calls = []

    def configure(*args):
        assert status == 'passed', 'Unsupported Metal runtime must not be constructed'
        calls.append(model_id)
        return lambda ctx: True

    monkeypatch.setattr(import_module('photo_worker.runtime.'+runtime_module), runtime_builder, configure)
    if status == 'missing':
        with pytest.raises(service.WorkerConfigurationError, match='requires installed'):
            service.build_worker(config)
        assert calls == []
        return
    _, transport = service.build_worker(config)
    assert (operation in transport.executor.operations) is (status == 'passed')
    assert calls == ([model_id] if status == 'passed' else [])
    assert 'pair.fit.color' in transport.executor.operations


@pytest.fixture(autouse=True)
def installed_startup_schema(monkeypatch):
    # Constructor isolation only; installed SQL authority has separate tests.
    from photo_worker.runtime.database import TaskDatabase
    monkeypatch.setattr(TaskDatabase, 'media_renderer_policy_ready', lambda self: True)
    monkeypatch.setattr(TaskDatabase, 'task_result_fence_ready', lambda self: True)
    monkeypatch.setattr(TaskDatabase, 'editor_internal_dispatch_installed', lambda self, operation: True)

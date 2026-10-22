"""Authored process-composition scenarios, not physical qualification evidence."""
from types import SimpleNamespace
from uuid import uuid4
import pytest
from photo_worker.runtime import service
from photo_worker.runtime.executor import Operation
from photo_worker.v2.render_native_installation import NativeRenderInstallation


def config(tmp_path, resource='cpu', backend='cpu'):
    return service.WorkerConfig('unused-db', 'unused-amqp', uuid4(), resource,
        tmp_path, 'https://unused.invalid', tmp_path/'ca', tmp_path/'cert',
        tmp_path/'key', 1024, 1024, 1024, backend=backend)


def isolate_process(monkeypatch, *, precompose_ready=True):
    from photo_worker.operations.layers import adapter as layers
    from photo_worker.operations.pair.fit import color
    for name, value in tuple(vars(service).items()):
        if name.endswith('_operations') and callable(value):
            monkeypatch.setattr(service, name, lambda *args, **kwargs: [])
    monkeypatch.setattr(service, 'asset_operations',
        lambda *args: [Operation('test.cpu', 'cpu', lambda ctx: None)])
    monkeypatch.setattr(color, 'color_fit_operations',
        lambda *args: [Operation('test.gpu', 'gpu', lambda ctx: None)])
    monkeypatch.setattr(layers, 'layer_operations', lambda *args: [])
    monkeypatch.setattr(service, 'client_tls', lambda *args: object())
    requested = []
    def installed(operation):
        requested.append(operation)
        return operation != 'editor.layers_precompose' or precompose_ready
    database = SimpleNamespace(media_renderer_policy_ready=lambda: True,
        task_result_fence_ready=lambda: True, editor_internal_dispatch_installed=installed)
    monkeypatch.setattr(service, 'TaskDatabase', lambda *args: database)
    monkeypatch.setattr(service, 'Transport',
        lambda url, resource, executor, **kwargs: SimpleNamespace(executor=executor))
    return database, requested


def native_cpu(monkeypatch):
    # Construction qualification is outside this composition test. Retain the
    # real concrete type and real precompose operation/admission constructors.
    native = object.__new__(NativeRenderInstallation)
    native.backend = 'cpu'
    def forbidden(*args):
        raise AssertionError('CPU precompose must not construct a GPU renderer')
    monkeypatch.setattr(NativeRenderInstallation, 'worker', forbidden)
    return native


def test_cpu_native_installation_mounts_precompose_not_gpu_compose(monkeypatch, tmp_path):
    database, requested = isolate_process(monkeypatch)
    returned, transport = service.build_worker(config(tmp_path), render_renderer=native_cpu(monkeypatch))
    assert returned is database
    assert 'editor.layers_precompose' in requested
    assert transport.executor.operations['editor.layers_precompose'].resource_class == 'cpu'
    assert 'render.compose' not in transport.executor.operations
    assert 'editor.effects_seal' in transport.executor.operations


def test_cpu_precompose_keeps_installed_sql_gate(monkeypatch, tmp_path):
    _, requested = isolate_process(monkeypatch, precompose_ready=False)
    with pytest.raises(service.WorkerConfigurationError, match='precompose authority'):
        service.build_worker(config(tmp_path), render_renderer=native_cpu(monkeypatch))
    assert 'editor.layers_precompose' in requested


def test_arbitrary_cpu_renderer_rejected_before_io(monkeypatch, tmp_path):
    def forbidden(*args):
        raise AssertionError('Invalid renderer must be refused before TLS/database')
    monkeypatch.setattr(service, 'client_tls', forbidden)
    monkeypatch.setattr(service, 'TaskDatabase', forbidden)
    with pytest.raises(service.WorkerConfigurationError, match='GPU worker backend'):
        service.build_worker(config(tmp_path), render_renderer=SimpleNamespace(backend='cpu'))


def test_gpu_renderer_still_mounts_gpu_compose(monkeypatch, tmp_path):
    isolate_process(monkeypatch)
    renderer = SimpleNamespace(backend='cuda', verify_runtime=lambda: None)
    _, transport = service.build_worker(config(tmp_path, 'gpu', 'cuda'), render_renderer=renderer)
    assert transport.executor.operations['render.compose'].resource_class == 'gpu'
    assert 'editor.layers_precompose' not in transport.executor.operations


def test_native_cpu_renderer_cannot_enter_gpu_worker(monkeypatch, tmp_path):
    with pytest.raises(service.WorkerConfigurationError, match='GPU worker backend'):
        service.build_worker(config(tmp_path, 'gpu', 'cuda'), render_renderer=native_cpu(monkeypatch))

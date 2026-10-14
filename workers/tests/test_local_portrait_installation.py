from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.runtime import local_portrait_config, service
from photo_worker.runtime.database import TaskDatabase
from photo_worker.operations.portrait import local_runtime_lock, local_sql_origin


SHA = 'a' * 64


def settings(**overrides):
    values = {
        'PHOTO_LOCAL_PORTRAIT_ENABLED': 'true',
        'PHOTO_LOCAL_PORTRAIT_RUNTIME_LOCK': '/qualified/local-portrait.json',
        'PHOTO_LOCAL_PORTRAIT_RUNTIME_SHA': SHA,
    }
    values.update(overrides)
    return values.get


def cpu_config():
    return SimpleNamespace(resource_class='cpu', backend='cpu', capacity_policy=None)


def test_local_portrait_is_absent_without_explicit_deployment(monkeypatch):
    read = Mock(side_effect=AssertionError('disabled runtime read lock'))
    monkeypatch.setattr(local_portrait_config, 'load_local_portrait_runtime_lock', read)
    assert local_portrait_config.local_portrait_installation_from_environment(cpu_config(),
        settings(PHOTO_LOCAL_PORTRAIT_ENABLED='false')) is None
    read.assert_not_called()


def test_local_portrait_requires_cpu_and_pinned_installed_bytes(monkeypatch):
    read = Mock(return_value=object())
    monkeypatch.setattr(local_portrait_config, 'load_local_portrait_runtime_lock', read)
    for config in (SimpleNamespace(resource_class='gpu', backend='cuda'),
                   SimpleNamespace(resource_class='cpu', backend='mps')):
        with pytest.raises(service.WorkerConfigurationError, match='CPU worker'):
            local_portrait_config.local_portrait_installation_from_environment(config, settings())
    for change in ({'PHOTO_LOCAL_PORTRAIT_RUNTIME_LOCK': 'relative.json'},
                   {'PHOTO_LOCAL_PORTRAIT_RUNTIME_SHA': 'BAD'},
                   {'PHOTO_LOCAL_PORTRAIT_ENABLED': 'yes'}):
        with pytest.raises(service.WorkerConfigurationError):
            local_portrait_config.local_portrait_installation_from_environment(cpu_config(), settings(**change))
    read.assert_not_called()
    installed = local_portrait_config.local_portrait_installation_from_environment(cpu_config(), settings())
    assert isinstance(installed, local_portrait_config.LocalPortraitInstallation)
    assert installed.runtime_sha == SHA
    read.assert_called_once()


def test_local_portrait_unqualified_lock_refuses_startup(monkeypatch):
    monkeypatch.setattr(local_portrait_config, 'load_local_portrait_runtime_lock',
        Mock(side_effect=RuntimeError('RUNTIME_BYTES_MISMATCH')))
    with pytest.raises(service.WorkerConfigurationError, match='Unqualified local portrait'):
        local_portrait_config.local_portrait_installation_from_environment(cpu_config(), settings())


def test_run_worker_passes_only_explicit_local_portrait_installation(monkeypatch):
    installed = local_portrait_config.LocalPortraitInstallation(object(), SHA)
    build = Mock(return_value=('database', 'transport'))
    run = Mock()
    monkeypatch.setattr(service, '_install_metal_budget', Mock(return_value=None))
    monkeypatch.setattr(service, 'build_worker', build)
    monkeypatch.setattr(service, 'run_built_worker', run)
    service.run_worker(cpu_config(), local_portrait_installation=installed)
    assert build.call_args.kwargs['local_portrait_installation'] is installed
    run.assert_called_once_with(cpu_config(), 'database', 'transport', metal_budget=None)


def test_qualified_sql_manual_installation_registers_both_v28_operations(monkeypatch):
    verify = Mock(return_value=SHA)
    monkeypatch.setattr(local_runtime_lock, 'verify_installed_runtime', verify)
    database = SimpleNamespace(local_portrait_lease_ready=Mock(return_value=True),
        local_portrait_manual_origin=Mock())
    lock = object()
    operations = local_sql_origin.sql_manual_local_portrait_operations(object(), database, lock, SHA)
    assert {(operation.name, operation.resource_class) for operation in operations} == {
        ('portrait.hair', 'cpu'), ('portrait.clothing', 'cpu')}
    verify.assert_called_once_with(lock, SHA)


def test_local_portrait_sql_installation_requires_worker_only_owner_functions(monkeypatch):
    database = TaskDatabase('unused')
    accepted = ('photo_fn_owner', True, 'search_path=pg_catalog, photo', True, False, False)
    query = Mock(side_effect=[accepted, accepted])
    monkeypatch.setattr(database, '_query', query)
    assert database.local_portrait_runtime_ready() is True
    assert query.call_count == 2
    query = Mock(side_effect=[accepted, (*accepted[:4], True, False)])
    monkeypatch.setattr(database, '_query', query)
    assert database.local_portrait_runtime_ready() is False
    query = Mock(return_value=None)
    monkeypatch.setattr(database, '_query', query)
    assert database.local_portrait_runtime_ready() is False

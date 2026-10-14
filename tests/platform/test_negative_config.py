from types import SimpleNamespace
import pytest
from photo_worker.runtime import negative_config as module
from photo_worker.runtime.service import WorkerConfigurationError


def test_negative_disabled_needs_no_configuration():
    assert module.negative_options_from_environment(None, {}.get) is None


@pytest.mark.parametrize('field,value', [
    ('PHOTO_NEGATIVE_ENABLED', 'yes'),
    ('PHOTO_NEGATIVE_RUNTIME_SHA', 'A'*64),
    ('PHOTO_NEGATIVE_RUNTIME_LOCK', 'relative.json'),
    ('PHOTO_NEGATIVE_SOURCE_MAX_BYTES', '200'),
    ('PHOTO_NEGATIVE_RAM_MAX_BYTES', '0'),
    ('PHOTO_NEGATIVE_RAM_MAX_BYTES', '9007199254740992'),
])
def test_negative_invalid_configuration_before_loading(monkeypatch, field, value):
    values = dict(PHOTO_NEGATIVE_ENABLED='true', PHOTO_NEGATIVE_RUNTIME_SHA='a'*64,
        PHOTO_NEGATIVE_RUNTIME_LOCK='/operator/negative.json',
        PHOTO_NEGATIVE_SOURCE_MAX_BYTES='100', PHOTO_NEGATIVE_RAM_MAX_BYTES='1000')
    values[field] = value
    def forbidden(*args):
        pytest.fail('Invalid configuration reached runtime loader')
    monkeypatch.setattr(module, 'load_negative_runtime_lock', forbidden)
    with pytest.raises(WorkerConfigurationError):
        module.negative_options_from_environment(SimpleNamespace(resource_class='cpu', backend='cpu', max_scratch_bytes=200), values.get)


def test_negative_options_load_exact_operator_pin(monkeypatch):
    calls = []
    lock = object()
    monkeypatch.setattr(module, 'load_negative_runtime_lock', lambda path, digest: calls.append((str(path), digest)) or lock)
    values = dict(PHOTO_NEGATIVE_ENABLED='true', PHOTO_NEGATIVE_RUNTIME_SHA='a'*64,
        PHOTO_NEGATIVE_RUNTIME_LOCK='/operator/negative.json',
        PHOTO_NEGATIVE_SOURCE_MAX_BYTES='100', PHOTO_NEGATIVE_RAM_MAX_BYTES='1000')
    options = module.negative_options_from_environment(SimpleNamespace(resource_class='cpu', backend='cpu', max_scratch_bytes=200), values.get)
    assert options.runtime_lock is lock and options.source_max_bytes == 100 and options.ram_max_bytes == 1000
    assert calls == [('/operator/negative.json', 'a'*64)]


def test_negative_installation_requires_icc_before_object_client(monkeypatch):
    monkeypatch.setattr(module, 'negative_options_from_environment', lambda *args: object())
    from photo_worker.runtime import donor_config
    def forbidden(*args):
        pytest.fail('Missing ICC reached object client')
    monkeypatch.setattr(donor_config, 'qualified_object_storage_from_environment', forbidden)
    with pytest.raises(WorkerConfigurationError,match='ICC'):
        module.negative_installation_from_environment(SimpleNamespace(icc_root=None))


def test_negative_installation_uses_verified_icc_and_storage(monkeypatch):
    from pathlib import Path
    from photo_worker.runtime import donor_config
    options=object();objects=object()
    monkeypatch.setattr(module, 'negative_options_from_environment', lambda *args: options)
    monkeypatch.setattr(donor_config, 'qualified_object_storage_from_environment', lambda *args: (objects,'bucket'))
    installation=module.negative_installation_from_environment(SimpleNamespace(
        icc_root=Path(__file__).resolve().parents[2]/'resources/icc'))
    assert installation.objects is objects and installation.options is options and installation.bucket=='bucket'

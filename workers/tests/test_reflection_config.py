"""V28 reflection is installed only from exact operator-owned model/runtime pins."""

from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.runtime import reflection_config
from photo_worker.runtime.service import WorkerConfigurationError


def config(tmp_path: Path):
    model_lock = tmp_path / 'model-lock.json'
    model_lock.write_bytes(b'operator model lock')
    return SimpleNamespace(resource_class='gpu', backend='cpu',
        model_root=tmp_path, model_lock=model_lock)


def test_reflection_disabled_without_operator_flag(tmp_path):
    assert reflection_config.reflection_installation_from_environment(
        config(tmp_path), {}.get) is None


def test_reflection_refuses_missing_pins_and_wrong_worker(tmp_path):
    worker = config(tmp_path)
    with pytest.raises(WorkerConfigurationError, match='pins'):
        reflection_config.reflection_installation_from_environment(worker,
            {'PHOTO_REFLECTION_ENABLED': 'true'}.get)
    worker.backend = 'mps'
    assert reflection_config.reflection_installation_from_environment(worker,
        {'PHOTO_REFLECTION_ENABLED': 'true'}.get) is None


def test_reflection_pins_existing_model_and_runtime_before_registration(monkeypatch, tmp_path):
    worker = config(tmp_path)
    model_sha, runtime_sha = 'a' * 64, 'b' * 64
    entry = SimpleNamespace(id='reflection_rgb', approved_for_install=True,
        backends={'cpu': SimpleNamespace(status='passed')})
    monkeypatch.setattr(reflection_config.ModelLock, 'model_validate_json',
                        lambda _: SimpleNamespace(lock_id=model_sha, models=[entry]))
    verify = Mock(return_value={'model.onnx': tmp_path / 'model.onnx'})
    load = Mock(return_value=object())
    monkeypatch.setattr(reflection_config, 'verify_model', verify)
    monkeypatch.setattr(reflection_config, 'load_reflection_runtime_lock', load)
    lock_path = tmp_path / 'runtime-lock.json'
    env = {'PHOTO_REFLECTION_ENABLED': 'true',
           'PHOTO_REFLECTION_MODEL_LOCK_SHA': model_sha,
           'PHOTO_REFLECTION_RUNTIME_SHA': runtime_sha,
           'PHOTO_REFLECTION_RUNTIME_LOCK': str(lock_path)}
    result = reflection_config.reflection_installation_from_environment(worker, env.get)
    assert result.model_lock_sha == model_sha and result.runtime_sha == runtime_sha
    verify.assert_called_once_with(tmp_path / model_sha / 'reflection_rgb', entry)
    load.assert_called_once_with(lock_path, runtime_sha, 'cpu')
    env['PHOTO_REFLECTION_MODEL_LOCK_SHA'] = 'c' * 64
    with pytest.raises(WorkerConfigurationError, match='Unqualified'):
        reflection_config.reflection_installation_from_environment(worker, env.get)

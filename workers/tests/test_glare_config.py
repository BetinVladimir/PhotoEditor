"""Donor glare is enabled only with exact runtime/model and bounded material."""

from pathlib import Path
from types import SimpleNamespace
from unittest.mock import Mock

import pytest

from photo_worker.runtime import glare_config
from photo_worker.runtime.service import WorkerConfigurationError


def config(tmp_path: Path):
    model_lock = tmp_path / 'models.json'
    model_lock.write_bytes(b'operator model lock')
    return SimpleNamespace(resource_class='gpu', backend='cpu',
        model_root=tmp_path, model_lock=model_lock, max_scratch_bytes=4096)


def test_donor_glare_disabled_by_default(tmp_path):
    assert glare_config.glare_donor_installation_from_environment(
        config(tmp_path), {}.get) is None


def test_donor_glare_refuses_wrong_worker_and_missing_pins(tmp_path):
    worker = config(tmp_path)
    with pytest.raises(WorkerConfigurationError, match='pins'):
        glare_config.glare_donor_installation_from_environment(worker,
            {'PHOTO_GLARE_DONOR_ENABLED': 'true'}.get)
    worker.backend = 'cuda'
    with pytest.raises(WorkerConfigurationError, match='CPU GPU-class'):
        glare_config.glare_donor_installation_from_environment(worker,
            {'PHOTO_GLARE_DONOR_ENABLED': 'true'}.get)


def test_donor_glare_binds_pinned_model_runtime_and_object_store(monkeypatch, tmp_path):
    from photo_worker.runtime import donor_config
    worker = config(tmp_path)
    model_sha, runtime_sha = 'a' * 64, 'b' * 64
    entry = SimpleNamespace(id='glare_mask', approved_for_install=True,
        backends={'cpu': SimpleNamespace(status='passed')})
    monkeypatch.setattr(glare_config.ModelLock, 'model_validate_json',
                        lambda _: SimpleNamespace(lock_id=model_sha, models=[entry]))
    verify = Mock(return_value={'model.onnx': tmp_path / 'model.onnx'})
    monkeypatch.setattr(glare_config, 'verify_model', verify)
    monkeypatch.setattr(glare_config, 'load_glare_donor_runtime_lock',
                        lambda *_: object())
    objects = SimpleNamespace(get_object=Mock())
    monkeypatch.setattr(donor_config, 'qualified_object_storage_from_environment',
                        lambda *_: (objects, 'photos'))
    env = {'PHOTO_GLARE_DONOR_ENABLED': 'true',
           'PHOTO_GLARE_DONOR_MODEL_LOCK_SHA': model_sha,
           'PHOTO_GLARE_DONOR_RUNTIME_SHA': runtime_sha,
           'PHOTO_GLARE_DONOR_RUNTIME_LOCK': str(tmp_path / 'runtime.json'),
           'PHOTO_GLARE_DONOR_MATERIAL_SCRATCH_ROOT': str(tmp_path),
           'PHOTO_GLARE_DONOR_MATERIAL_MAX_BYTES': '2048'}
    installed = glare_config.glare_donor_installation_from_environment(worker, env.get)
    assert installed.bucket == 'photos' and installed.material_max_bytes == 2048
    assert installed.model_lock_sha == model_sha and installed.runtime_sha == runtime_sha
    verify.assert_called_once_with(tmp_path / model_sha / 'glare_mask', entry)
    env['PHOTO_GLARE_DONOR_MATERIAL_MAX_BYTES'] = '8192'
    with pytest.raises(WorkerConfigurationError, match='scratch'):
        glare_config.glare_donor_installation_from_environment(worker, env.get)

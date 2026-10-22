from types import SimpleNamespace
import pytest
from photo_worker.runtime.removal_preview_config import removal_preview_from_environment
from photo_worker.runtime.service import WorkerConfigurationError


def test_removal_preview_disabled_needs_no_model_or_storage():
    assert removal_preview_from_environment(SimpleNamespace(),lambda key,default=None:default) is None


@pytest.mark.parametrize('backend,resource',[('cpu','cpu'),('metal','gpu'),('cuda','cpu')])
def test_removal_preview_requires_supported_gpu_class_worker(backend,resource):
    config=SimpleNamespace(backend=backend,resource_class=resource)
    with pytest.raises(WorkerConfigurationError,match='GPU-class worker'):
        removal_preview_from_environment(config,lambda key,default=None:'true' if key=='PHOTO_REMOVAL_PREVIEW_ENABLED' else default)


@pytest.mark.parametrize('backend',['cpu','cuda','mps'])
def test_removal_preview_supported_backend_requires_own_pins(backend):
    config=SimpleNamespace(backend=backend,resource_class='gpu',icc_root=None)
    with pytest.raises(WorkerConfigurationError,match='measured runtime hashes'):
        removal_preview_from_environment(config,lambda key,default=None:'true' if key=='PHOTO_REMOVAL_PREVIEW_ENABLED' else default)


def test_removal_preview_rejects_ambiguous_enablement():
    with pytest.raises(WorkerConfigurationError):
        removal_preview_from_environment(SimpleNamespace(),lambda key,default=None:'1')


def test_removal_startup_binds_actual_model_lock_to_declared_pin(tmp_path):
    from photo_worker.contracts.base import canonical_bytes, canonical_hash
    from photo_worker.runtime.removal_preview_config import _require_removal_model_lock
    payload = {'schema_version': 1, 'models': []}
    expected = canonical_hash(payload)
    path = tmp_path / 'models.lock.json'
    path.write_bytes(canonical_bytes(dict(payload, lock_id=expected)))
    config = SimpleNamespace(model_lock=path, model_root=tmp_path)
    _require_removal_model_lock(config, expected)
    with pytest.raises(WorkerConfigurationError, match='does not match qualification'):
        _require_removal_model_lock(config, 'f'*64)
    path.write_bytes(b'{invalid')
    with pytest.raises(WorkerConfigurationError, match='does not match qualification'):
        _require_removal_model_lock(config, expected)
    path.unlink()
    with pytest.raises(WorkerConfigurationError, match='does not match qualification'):
        _require_removal_model_lock(config, expected)


def test_removal_startup_rejects_symlinked_lock(tmp_path):
    from photo_worker.runtime.removal_preview_config import _require_removal_model_lock
    target = tmp_path / 'target.json'
    target.write_bytes(b'{}')
    path = tmp_path / 'models.lock.json'
    path.symlink_to(target)
    with pytest.raises(WorkerConfigurationError, match='does not match qualification'):
        _require_removal_model_lock(SimpleNamespace(model_lock=path, model_root=tmp_path), 'a'*64)

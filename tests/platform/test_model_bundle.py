import hashlib
import json
from pathlib import Path

import pytest
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.models.bundle import create_bundle, verify_bundle
from photo_worker.models.install import ModelError, install_models
from photo_worker.models.lock import ModelLock


def fixture_lock(path: Path, payload: bytes) -> ModelLock:
    entry = {
        'id': 'lraspp_mobilenet_v3_large', 'architecture': 'test-lock-only',
        'weight_id': 'fixture', 'repository': 'https://download.pytorch.org/models/',
        'revision': 'fixture1',
        'files': [{'path': 'weights.bin', 'bytes': len(payload),
                   'sha256': hashlib.sha256(payload).hexdigest(),
                   'url': 'https://download.pytorch.org/models/fixture.bin'}],
        'preprocessing_id': 'test', 'packages_lock_hash': 'a' * 64,
        'code_license': 'CC0-1.0', 'weights_license': 'CC0-1.0',
        'notices': [], 'redistribution_policy': 'allowed',
        'approved_for_install': True,
        'backends': {backend: {'status': 'untested', 'reason': 'fixture',
                               'report_sha256': None} for backend in ('cpu', 'cuda', 'mps')},
    }
    body = {'schema_version': 1, 'models': [entry]}
    lock = ModelLock.model_validate({**body, 'lock_id': canonical_hash(body)})
    path.write_bytes(canonical_bytes(lock.model_dump(mode='json')))
    return lock


def test_offline_bundle_is_reproducible_and_installs_without_network(tmp_path):
    payload = b'fixture weights, never a production model'
    lock_path = tmp_path / 'lock.json'
    lock = fixture_lock(lock_path, payload)
    root = tmp_path / 'installed'
    model_path = root / lock.lock_id / lock.models[0].id
    model_path.mkdir(parents=True)
    (model_path / 'weights.bin').write_bytes(payload)
    bundle = tmp_path / 'bundle'
    manifest = create_bundle(lock_path, root, [lock.models[0].id], bundle)
    assert verify_bundle(bundle) == manifest
    destination = tmp_path / 'new-install'
    installed = install_models(lock, [lock.models[0].id], destination, bundle)
    assert (installed[lock.models[0].id] / 'weights.bin').read_bytes() == payload
    with pytest.raises(ModelError, match='DESTINATION_INVALID'):
        create_bundle(lock_path, root, [lock.models[0].id], bundle)


def test_offline_bundle_detects_tampering_and_never_publishes_partial_output(tmp_path):
    payload = b'fixture weights'
    lock_path = tmp_path / 'lock.json'
    lock = fixture_lock(lock_path, payload)
    root = tmp_path / 'installed'
    model_path = root / lock.lock_id / lock.models[0].id
    model_path.mkdir(parents=True)
    weight = model_path / 'weights.bin'
    weight.write_bytes(payload)
    weight.write_bytes(b'forged bytes')
    bundle = tmp_path / 'bundle'
    with pytest.raises(ModelError, match='HASH_MISMATCH'):
        create_bundle(lock_path, root, [lock.models[0].id], bundle)
    assert not bundle.exists()
    weight.write_bytes(payload)
    create_bundle(lock_path, root, [lock.models[0].id], bundle)
    exported = bundle / lock.models[0].id / 'weights.bin'
    exported.chmod(0o600)
    exported.write_bytes(b'forged bytes')
    with pytest.raises(ModelError, match='HASH_MISMATCH'):
        verify_bundle(bundle)
    exported.write_bytes(payload)
    manifest_path = bundle / 'bundle-manifest.json'
    manifest = json.loads(manifest_path.read_bytes())
    manifest['models'][0]['files'][0]['sha256'] = '0' * 64
    manifest_path.write_bytes(canonical_bytes(manifest))
    with pytest.raises(ModelError, match='BUNDLE_INVALID'):
        verify_bundle(bundle)


def test_bundle_rejects_symlinked_installed_weights(tmp_path):
    payload = b'fixture weights'
    lock_path = tmp_path / 'lock.json'
    lock = fixture_lock(lock_path, payload)
    root = tmp_path / 'installed'
    model_path = root / lock.lock_id / lock.models[0].id
    model_path.mkdir(parents=True)
    (tmp_path / 'elsewhere.bin').write_bytes(payload)
    (model_path / 'weights.bin').symlink_to(tmp_path / 'elsewhere.bin')
    with pytest.raises(ModelError, match='MODEL_UNAVAILABLE'):
        create_bundle(lock_path, root, [lock.models[0].id], tmp_path / 'bundle')


def test_bundle_rejects_unlisted_files_and_malformed_manifest(tmp_path):
    payload = b'fixture weights'
    lock_path = tmp_path / 'lock.json'
    lock = fixture_lock(lock_path, payload)
    root = tmp_path / 'installed'
    model_path = root / lock.lock_id / lock.models[0].id
    model_path.mkdir(parents=True)
    (model_path / 'weights.bin').write_bytes(payload)
    bundle = tmp_path / 'bundle'
    create_bundle(lock_path, root, [lock.models[0].id], bundle)
    extra = bundle / 'unexpected.bin'
    extra.write_bytes(b'extra')
    with pytest.raises(ModelError, match='BUNDLE_INVALID'):
        verify_bundle(bundle)
    extra.unlink()
    (bundle / 'bundle-manifest.json').write_bytes(b'[]')
    with pytest.raises(ModelError, match='BUNDLE_INVALID'):
        verify_bundle(bundle)
    (bundle / 'bundle-manifest.json').write_bytes(b'{"schema_version":1,"schema_version":1}')
    with pytest.raises(ModelError, match='BUNDLE_INVALID'):
        verify_bundle(bundle)

"""Create and verify an offline, hash-checked model bundle.

The bundle contains only bytes named by an approved ModelLock. It is not a
qualification report: runtime/backend evidence is checked separately.
"""
from __future__ import annotations

import hashlib
import os
import shutil
import tempfile
from pathlib import Path

from photo_worker.contracts.base import canonical_bytes, parse_json

from .install import ModelError, file_hash, verify_model
from .lock import ModelLock


def _model_root(path: Path) -> Path:
    if path.is_symlink() or not path.is_dir():
        raise ModelError('MODEL_PATH_INVALID')
    return path.resolve(strict=True)


def create_bundle(lock_path: Path, model_root: Path, selected_ids: list[str], destination: Path) -> dict:
    """Copy verified installed weights into a new directory, then publish it atomically.

    ``destination`` must not exist. No network access or weight substitution is
    performed. The returned manifest describes bytes, not model quality.
    """
    if len(set(selected_ids)) != len(selected_ids) or not selected_ids:
        raise ModelError('MODEL_SELECTION_INVALID')
    if lock_path.is_symlink() or not lock_path.is_file() or lock_path.stat().st_size > 8_388_608:
        raise ModelError('MODEL_LOCK_INVALID')
    raw = lock_path.read_bytes()
    lock = ModelLock.model_validate_json(raw)
    entries = {entry.id: entry for entry in lock.models}
    if not set(selected_ids) <= entries.keys():
        raise ModelError('MODEL_UNAVAILABLE')
    for model_id in selected_ids:
        if not entries[model_id].approved_for_install:
            raise ModelError('MODEL_NOT_APPROVED')
    root = _model_root(model_root)
    target = destination.absolute()
    if target.exists() or target.is_symlink() or not target.parent.is_dir() or target.parent.is_symlink():
        raise ModelError('MODEL_BUNDLE_DESTINATION_INVALID')
    if target.parent.resolve(strict=True).is_relative_to(root):
        raise ModelError('MODEL_BUNDLE_DESTINATION_INVALID')

    manifest = {
        'schema_version': 1,
        'lock_id': lock.lock_id,
        'lock_file_sha256': hashlib.sha256(raw).hexdigest(),
        'models': [],
    }
    staging = Path(tempfile.mkdtemp(prefix='.model-bundle-', dir=target.parent))
    try:
        for model_id in sorted(selected_ids):
            entry = entries[model_id]
            files = verify_model(root / lock.lock_id / model_id, entry)
            copied = []
            for spec in sorted(entry.files, key=lambda value: value.path):
                source = files[spec.path]
                output = staging / model_id / spec.path
                output.parent.mkdir(parents=True, exist_ok=True)
                with source.open('rb') as reader, output.open('xb') as writer:
                    shutil.copyfileobj(reader, writer, length=1_048_576)
                    writer.flush()
                    os.fsync(writer.fileno())
                if output.stat().st_size != spec.bytes or file_hash(output) != spec.sha256:
                    raise ModelError('MODEL_HASH_MISMATCH')
                output.chmod(0o444)
                copied.append({'path': spec.path, 'bytes': spec.bytes, 'sha256': spec.sha256})
            manifest['models'].append({'id': model_id, 'files': copied})
        (staging / 'model-lock.json').write_bytes(raw)
        (staging / 'bundle-manifest.json').write_bytes(canonical_bytes(manifest) + b'\n')
        # Verification before rename ensures the published directory is complete.
        verify_bundle(staging)
        if target.exists() or target.is_symlink():
            raise ModelError('MODEL_BUNDLE_DESTINATION_INVALID')
        os.rename(staging, target)
        return manifest
    finally:
        if staging.exists():
            shutil.rmtree(staging)


def verify_bundle(bundle: Path) -> dict:
    """Verify a bundle against its embedded, hash-checked ModelLock."""
    root = _model_root(bundle)
    lock_path = root / 'model-lock.json'
    manifest_path = root / 'bundle-manifest.json'
    if any(path.is_symlink() or not path.is_file() or path.stat().st_size > 8_388_608
           for path in (lock_path, manifest_path)):
        raise ModelError('MODEL_BUNDLE_INVALID')
    raw = lock_path.read_bytes()
    lock = ModelLock.model_validate_json(raw)
    try:
        manifest = parse_json(manifest_path.read_bytes(), max_bytes=8_388_608)
    except (ValueError, UnicodeError) as exc:
        raise ModelError('MODEL_BUNDLE_INVALID') from exc
    if not isinstance(manifest, dict) or set(manifest) != {'schema_version', 'lock_id', 'lock_file_sha256', 'models'} \
       or manifest['schema_version'] != 1 or manifest['lock_id'] != lock.lock_id \
       or manifest['lock_file_sha256'] != hashlib.sha256(raw).hexdigest() \
       or not isinstance(manifest['models'], list) or not manifest['models']:
        raise ModelError('MODEL_BUNDLE_INVALID')
    entries = {entry.id: entry for entry in lock.models}
    ids = []
    for item in manifest['models']:
        if not isinstance(item, dict) or set(item) != {'id', 'files'} or not isinstance(item['id'], str) or item['id'] not in entries:
            raise ModelError('MODEL_BUNDLE_INVALID')
        model_id = item['id']
        ids.append(model_id)
        entry = entries[model_id]
        if not entry.approved_for_install:
            raise ModelError('MODEL_NOT_APPROVED')
        expected = [{'path': spec.path, 'bytes': spec.bytes, 'sha256': spec.sha256}
                    for spec in sorted(entry.files, key=lambda value: value.path)]
        if item['files'] != expected:
            raise ModelError('MODEL_BUNDLE_INVALID')
        verify_model(root / model_id, entry)
    if ids != sorted(set(ids)):
        raise ModelError('MODEL_BUNDLE_INVALID')
    expected_paths = {'model-lock.json', 'bundle-manifest.json'}
    for item in manifest['models']:
        expected_paths.update(f"{item['id']}/{spec['path']}" for spec in item['files'])
    actual_paths = set()
    for path in root.rglob('*'):
        if path.is_symlink():
            raise ModelError('MODEL_BUNDLE_INVALID')
        if path.is_file():
            actual_paths.add(path.relative_to(root).as_posix())
        elif not path.is_dir():
            raise ModelError('MODEL_BUNDLE_INVALID')
    if actual_paths != expected_paths:
        raise ModelError('MODEL_BUNDLE_INVALID')
    return manifest

"""Reflection runtime lock binds package and source bytes before installation."""

from hashlib import sha256
from pathlib import Path
import platform
import sys

import pytest
from typer.testing import CliRunner

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.operations.image import reflection_runtime_lock as runtime
from photo_worker.cli import app


def pinned_lock(monkeypatch, tmp_path: Path):
    source = tmp_path / 'reflection.py'
    source.write_bytes(b'pinned runtime source')
    monkeypatch.setattr(runtime, 'component_paths', lambda: {'reflection': source})
    monkeypatch.setattr(runtime.metadata, 'version', lambda _: '1.0')
    lock = runtime.ReflectionRuntimeLock(schema_version=1, backend='cpu',
        python_version='.'.join(map(str, sys.version_info[:3])),
        implementation='CPython', machine=platform.machine(),
        packages={name: '1.0' for name in
            ('numpy', 'pydantic', 'rfc8785', 'opencv-python-headless', 'onnxruntime')},
        components={'reflection': sha256(source.read_bytes()).hexdigest()})
    return lock, source


def test_reflection_runtime_lock_rejects_changed_source_or_backend(monkeypatch, tmp_path):
    lock, source = pinned_lock(monkeypatch, tmp_path)
    digest = canonical_hash(lock)
    assert runtime.verify_installed_runtime(lock, digest, 'cpu') == digest
    with pytest.raises(ValueError, match='LOCK_MISMATCH'):
        runtime.verify_installed_runtime(lock, digest, 'cuda')
    source.write_bytes(b'changed source')
    with pytest.raises(ValueError, match='BYTES_MISMATCH'):
        runtime.verify_installed_runtime(lock, digest, 'cpu')


def test_reflection_runtime_lock_loads_only_pinned_canonical_regular_file(monkeypatch, tmp_path):
    lock, _ = pinned_lock(monkeypatch, tmp_path)
    path = tmp_path / 'runtime-lock.json'
    path.write_bytes(canonical_bytes(lock))
    digest = canonical_hash(lock)
    assert runtime.load_reflection_runtime_lock(path, digest, 'cpu') == lock
    with pytest.raises(ValueError, match='LOCK_MISMATCH'):
        runtime.load_reflection_runtime_lock(path, '0' * 64, 'cpu')


def test_reflection_runtime_cli_reports_identity_without_qualification(monkeypatch, tmp_path):
    lock, _ = pinned_lock(monkeypatch, tmp_path)
    path = tmp_path / 'runtime-lock.json'
    path.write_bytes(canonical_bytes(lock))
    digest = canonical_hash(lock)
    result = CliRunner().invoke(app, ['verify-reflection-runtime', str(path), digest, 'cpu'])
    assert result.exit_code == 0
    assert '"qualification": "not_measured"' in result.stdout
    invalid = CliRunner().invoke(app, ['verify-reflection-runtime', str(path), digest, 'cuda'])
    assert invalid.exit_code == 2

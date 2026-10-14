"""Donor glare runtime identity binds source and packages but not qualification."""

from hashlib import sha256
import platform
import sys

import pytest
from typer.testing import CliRunner

from photo_worker.cli import app
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.operations.portrait import glare_donor_runtime_lock as runtime


def pinned_lock(monkeypatch, tmp_path):
    source = tmp_path / 'glare.py'
    source.write_bytes(b'pinned glare implementation')
    monkeypatch.setattr(runtime, 'component_paths', lambda: {'glare': source})
    monkeypatch.setattr(runtime.metadata, 'version', lambda _: '1.0')
    packages = {name: '1.0' for name in
        ('numpy', 'pydantic', 'rfc8785', 'opencv-python-headless', 'onnxruntime')}
    lock = runtime.GlareDonorRuntimeLock(schema_version=1,
        python_version='.'.join(map(str, sys.version_info[:3])),
        implementation='CPython', machine=platform.machine(),
        packages=packages,
        components={'glare': sha256(source.read_bytes()).hexdigest()})
    return lock, source


def test_glare_runtime_lock_refuses_changed_code_and_digest(monkeypatch, tmp_path):
    lock, source = pinned_lock(monkeypatch, tmp_path)
    digest = canonical_hash(lock)
    assert runtime.verify_installed_runtime(lock, digest) == digest
    with pytest.raises(ValueError, match='LOCK_MISMATCH'):
        runtime.verify_installed_runtime(lock, '0' * 64)
    source.write_bytes(b'changed implementation')
    with pytest.raises(ValueError, match='BYTES_MISMATCH'):
        runtime.verify_installed_runtime(lock, digest)


def test_glare_runtime_cli_reports_identity_without_qualification(monkeypatch, tmp_path):
    lock, _ = pinned_lock(monkeypatch, tmp_path)
    path = tmp_path / 'runtime-lock.json'
    path.write_bytes(canonical_bytes(lock))
    digest = canonical_hash(lock)
    result = CliRunner().invoke(app, ['verify-glare-donor-runtime', str(path), digest])
    assert result.exit_code == 0
    assert '"qualification": "not_measured"' in result.stdout
    invalid = CliRunner().invoke(app, ['verify-glare-donor-runtime', str(path), '0' * 64])
    assert invalid.exit_code == 2

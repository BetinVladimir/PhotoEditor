"""Installed Render identity includes encoded-output adoption dependencies."""
from importlib import metadata
import platform
import sys
from pathlib import Path

import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.runtime.executor import OperationFailure
from photo_worker.v2 import render_runtime as runtime


def installed_lock():
    return runtime.RenderRuntimeLock(schema_version=1,
        python_version='.'.join(map(str,sys.version_info[:3])),
        implementation='CPython', machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},
        components=runtime.installed_component_hashes())


def test_render_lock_covers_encoded_adoption_and_verifies_installed_bytes():
    paths = runtime.component_paths()
    root = Path(runtime.__file__).resolve().parents[1]
    for name, relative in {
        'render_derivative_adoption':'v2/render_derivative_adoption.py',
        'render_derivative_store':'v2/render_derivative_store.py',
        'render_derivative_service':'v2/render_derivative_service.py',
        'render_derivative_schema':'v2/render_derivative_schema.py',
        'media_derivative_encoder':'operations/media/derivative.py',
    }.items():
        assert paths[name] == root/relative
    lock = installed_lock()
    assert runtime.verify_installed_runtime(lock,canonical_hash(lock)) == canonical_hash(lock)


def test_changed_derivative_verifier_invalidates_pinned_runtime(monkeypatch):
    lock = installed_lock()
    changed = dict(lock.components)
    changed['render_derivative_adoption'] = '0'*64
    monkeypatch.setattr(runtime,'installed_component_hashes',lambda:changed)
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        runtime.verify_installed_runtime(lock,canonical_hash(lock))


def test_old_inventory_requires_explicit_new_lock():
    lock = installed_lock().model_dump(mode='json')
    del lock['components']['render_derivative_schema']
    with pytest.raises(ValueError,match='RENDER_RUNTIME_INVENTORY_INVALID'):
        runtime.RenderRuntimeLock.model_validate(lock)

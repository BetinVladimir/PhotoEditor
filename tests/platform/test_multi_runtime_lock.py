from pathlib import Path
from types import SimpleNamespace
import sys

import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.optical import multi_runtime_lock as runtime
from photo_worker.runtime.executor import OperationFailure


def pinned(monkeypatch, tmp_path):
    binary = tmp_path / 'cv2.abi3.so'
    binary.write_bytes(b'controlled native runtime')
    monkeypatch.setattr(runtime, 'component_paths', lambda: {'opencv/cv2/cv2.abi3.so': binary})
    monkeypatch.setattr(runtime, 'verify_opencv_origin', lambda: None)
    def version(name):
        if name in runtime.OPENCV_DISTRIBUTIONS:
            raise runtime.metadata.PackageNotFoundError(name)
        return '1.2.3'
    monkeypatch.setattr(runtime.metadata, 'version', version)
    lock = runtime.MultiImageRuntimeLock(schema_version=1,
        python_version='.'.join(map(str, sys.version_info[:3])), implementation='CPython',
        machine=runtime.platform.machine(), packages={name: '1.2.3' for name in runtime.PACKAGES},
        components=runtime.installed_component_hashes())
    return lock, binary, version


def test_runtime_pin_checks_actual_component_bytes(monkeypatch, tmp_path):
    lock, binary, _ = pinned(monkeypatch, tmp_path)
    assert runtime.verify_installed_runtime(lock, canonical_hash(lock)) == canonical_hash(lock)
    binary.write_bytes(b'changed native runtime')
    with pytest.raises(OperationFailure, match='RUNTIME_BYTES_MISMATCH'):
        runtime.verify_installed_runtime(lock, canonical_hash(lock))


def test_runtime_pin_refuses_conflicts_versions_and_digest(monkeypatch, tmp_path):
    lock, _, version = pinned(monkeypatch, tmp_path)
    with pytest.raises(OperationFailure, match='RUNTIME_LOCK_MISMATCH'):
        runtime.verify_installed_runtime(lock, 'a' * 64)
    monkeypatch.setattr(runtime.metadata, 'version', lambda name: '1.2.3')
    with pytest.raises(OperationFailure, match='RUNTIME_UNQUALIFIED'):
        runtime.verify_installed_runtime(lock, canonical_hash(lock))
    def changed(name):
        if name == 'opencv-python-headless':
            return '9.0.0'
        return version(name)
    monkeypatch.setattr(runtime.metadata, 'version', changed)
    with pytest.raises(OperationFailure, match='RUNTIME_UNQUALIFIED'):
        runtime.verify_installed_runtime(lock, canonical_hash(lock))


def test_unvalidated_lock_and_missing_component_refused(monkeypatch, tmp_path):
    lock, binary, _ = pinned(monkeypatch, tmp_path)
    changed = lock.model_copy(update={'packages': {'numpy': '1.2.3'}})
    with pytest.raises(OperationFailure, match='RUNTIME_UNQUALIFIED'):
        runtime.verify_installed_runtime(changed, canonical_hash(changed))
    binary.unlink()
    with pytest.raises(OperationFailure, match='RUNTIME_UNQUALIFIED'):
        runtime.verify_installed_runtime(lock, canonical_hash(lock))


def test_opencv_inventory_includes_native_dependencies(monkeypatch, tmp_path):
    monkeypatch.setattr(runtime, 'correction_component_paths', lambda: {})
    files = [Path('cv2/__init__.py'), Path('cv2/cv2.abi3.so'),
             Path('opencv_python_headless.libs/libcodec.so.1'), Path('cv2/.dylibs/libcodec.dylib'),
             Path('unrelated/secret.py')]
    distribution = SimpleNamespace(files=files, locate_file=lambda p: tmp_path / p)
    monkeypatch.setattr(runtime.metadata, 'distribution', lambda name: distribution)
    paths = runtime.component_paths()
    assert paths['opencv/cv2/cv2.abi3.so'] == tmp_path / 'cv2/cv2.abi3.so'
    assert 'opencv/opencv_python_headless.libs/libcodec.so.1' in paths
    assert 'opencv/cv2/.dylibs/libcodec.dylib' in paths
    assert not any('unrelated' in key for key in paths)
    distribution.files = [Path('cv2/__init__.py')]
    with pytest.raises(ValueError, match='BINARY_MISSING'):
        runtime.component_paths()


def test_opencv_loaded_origin_must_be_pinned(monkeypatch, tmp_path):
    trusted = tmp_path / 'cv2' / '__init__.py'
    monkeypatch.setattr(runtime, 'component_paths', lambda: {'opencv/cv2/__init__.py': trusted})
    module = SimpleNamespace(__file__=str(trusted))
    monkeypatch.setitem(sys.modules, 'cv2', module)
    runtime.verify_opencv_origin()
    module.__file__ = str(tmp_path / 'foreign' / '__init__.py')
    with pytest.raises(OperationFailure, match='RUNTIME_BYTES_MISMATCH'):
        runtime.verify_opencv_origin()

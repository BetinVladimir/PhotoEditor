"""Exact installed code and package identity for V28 reflection_rgb."""

from hashlib import sha256
from importlib import metadata
from pathlib import Path
import os
import platform
import re
import stat
import sys
from typing import Literal

from pydantic import model_validator

from photo_worker.contracts.base import Contract, SHA256, canonical_bytes, canonical_hash, parse_json


def component_paths() -> dict[str, Path]:
    root = Path(__file__).resolve().parents[2]
    return {
        'reflection_onnx': root / 'models/reflection_onnx.py',
        'onnx_session': root / 'models/onnx_session.py',
        'backend_support': root / 'models/backend_support.py',
        'model_lock': root / 'models/lock.py',
        'model_pool': root / 'models/pool.py',
        'model_adapters': root / 'models/adapters.py',
        'reflection': root / 'operations/image/reflection.py',
        'reflection_admission': root / 'operations/image/reflection_admission.py',
        'reflection_runtime_lock': root / 'operations/image/reflection_runtime_lock.py',
        'reflection_inputs': root / 'operations/image/reflection_inputs.py',
        'reflection_tiles': root / 'operations/image/reflection_tiles.py',
        'reflection_prepare': root / 'operations/image/reflection_prepare.py',
        'reflection_result': root / 'operations/image/reflection_result.py',
        'reflection_color': root / 'algorithms/reflection_color.py',
        'safe_delta': root / 'algorithms/safe_delta.py',
        'model_delta': root / 'algorithms/model_delta.py',
        'image_contracts': root / 'contracts/images.py',
        'restoration_contracts': root / 'contracts/portrait_restoration.py',
        'runtime_database': root / 'runtime/database.py',
        'runtime_service': root / 'runtime/service.py',
        'worker_cli': root / 'cli.py',
        'runtime_reflection_config': root / 'runtime/reflection_config.py',
        'runtime_v28_installation': root / 'runtime/v28_installation.py',
    }


class ReflectionRuntimeLock(Contract):
    schema_version: Literal[1]
    backend: Literal['cpu', 'cuda']
    python_version: str
    implementation: Literal['CPython']
    machine: str
    packages: dict[str, str]
    components: dict[str, SHA256]

    @model_validator(mode='after')
    def inventory(self):
        runtime_package = 'onnxruntime-gpu' if self.backend == 'cuda' else 'onnxruntime'
        if (re.fullmatch(r'\d+\.\d+\.\d+', self.python_version) is None
                or not 1 <= len(self.machine) <= 80
                or set(self.packages) != {'numpy', 'pydantic', 'rfc8785',
                                         'opencv-python-headless', runtime_package}
                or any(not 1 <= len(value) <= 120 for value in self.packages.values())
                or set(self.components) != set(component_paths())):
            raise ValueError('REFLECTION_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock: ReflectionRuntimeLock, expected_sha: str,
                             backend: str) -> str:
    lock = ReflectionRuntimeLock.model_validate_json(canonical_bytes(lock))
    digest = canonical_hash(lock)
    if digest != expected_sha or lock.backend != backend:
        raise ValueError('REFLECTION_RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation() != lock.implementation
            or '.'.join(map(str, sys.version_info[:3])) != lock.python_version
            or platform.machine() != lock.machine):
        raise ValueError('REFLECTION_RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name) != version
               for name, version in lock.packages.items()):
            raise ValueError('REFLECTION_RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise ValueError('REFLECTION_RUNTIME_UNQUALIFIED') from error
    for name, path in component_paths().items():
        digest_file = sha256()
        with path.open('rb') as stream:
            for block in iter(lambda: stream.read(1_048_576), b''):
                digest_file.update(block)
        if digest_file.hexdigest() != lock.components[name]:
            raise ValueError('REFLECTION_RUNTIME_BYTES_MISMATCH')
    return digest


def load_reflection_runtime_lock(path: Path, expected_sha: str,
                                 backend: str) -> ReflectionRuntimeLock:
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not 1 <= info.st_size <= 65536:
                raise ValueError('REFLECTION_RUNTIME_LOCK_MISMATCH')
            raw = stream.read(65537)
        if len(raw) != info.st_size:
            raise ValueError('REFLECTION_RUNTIME_LOCK_MISMATCH')
        parsed = parse_json(raw, max_bytes=65536)
        if canonical_bytes(parsed) != raw:
            raise ValueError('REFLECTION_RUNTIME_LOCK_MISMATCH')
        lock = ReflectionRuntimeLock.model_validate_json(raw)
        verify_installed_runtime(lock, expected_sha, backend)
        return lock
    except OSError as error:
        raise ValueError('REFLECTION_RUNTIME_LOCK_MISMATCH') from error

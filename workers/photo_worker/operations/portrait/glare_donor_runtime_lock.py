"""Installed donor-glare implementation identity, separate from quality proof."""

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
from .donor_runtime_lock import component_paths as donor_component_paths


def component_paths() -> dict[str, Path]:
    paths = {'donor_' + name: path for name, path in donor_component_paths().items()}
    root = Path(__file__).resolve().parents[2]
    for name in ('glare', 'glare_inputs', 'glare_stage', 'glare_result',
                 'glare_donor', 'glare_donor_admission', 'glare_donor_runtime_lock',
                 'glare_generative_worker', 'glare_generative_inputs',
                 'glare_generative_stage', 'glare_generative_result',
                 'glare_generative_evidence', 'glare_generative_files',
                 'glare_generative_patch', 'glare_source_admission'):
        paths[name] = root / 'operations/portrait' / (name + '.py')
    for name in ('onnx_session', 'backend_support', 'capture_onnx', 'adapters', 'pool'):
        paths['model_' + name] = root / 'models' / (name + '.py')
    paths['glare_donor_object_reader'] = root / 'v2/glare_donor_object_reader.py'
    paths['runtime_v28_installation'] = root / 'runtime/v28_installation.py'
    paths['runtime_glare_config'] = root / 'runtime/glare_config.py'
    paths['worker_cli'] = root / 'cli.py'
    paths['restoration_contracts'] = root / 'contracts/portrait_restoration.py'
    paths['semantic_candidate_inpaint'] = root / 'operations/semantic/candidate_inpaint.py'
    return paths


class GlareDonorRuntimeLock(Contract):
    schema_version: Literal[1]
    python_version: str
    implementation: Literal['CPython']
    machine: str
    packages: dict[str, str]
    components: dict[str, SHA256]

    @model_validator(mode='after')
    def inventory(self):
        if (re.fullmatch(r'\d+\.\d+\.\d+', self.python_version) is None
                or not 1 <= len(self.machine) <= 80
                or set(self.packages) not in (
                    {'numpy', 'pydantic', 'rfc8785', 'opencv-python-headless', 'onnxruntime'},
                    {'numpy', 'pydantic', 'rfc8785', 'opencv-python-headless', 'onnxruntime-gpu'},
                    {'numpy', 'pydantic', 'rfc8785', 'opencv-python-headless', 'onnxruntime', 'torch'},
                    {'numpy', 'pydantic', 'rfc8785', 'opencv-python-headless', 'onnxruntime-gpu', 'torch'})
                or any(not 1 <= len(value) <= 120 for value in self.packages.values())
                or set(self.components) != set(component_paths())):
            raise ValueError('GLARE_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock: GlareDonorRuntimeLock,
                             expected_sha: str) -> str:
    lock = GlareDonorRuntimeLock.model_validate_json(canonical_bytes(lock))
    digest = canonical_hash(lock)
    if digest != expected_sha:
        raise ValueError('GLARE_RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation() != lock.implementation
            or '.'.join(map(str, sys.version_info[:3])) != lock.python_version
            or platform.machine() != lock.machine):
        raise ValueError('GLARE_RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name) != version
               for name, version in lock.packages.items()):
            raise ValueError('GLARE_RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise ValueError('GLARE_RUNTIME_UNQUALIFIED') from error
    for name, path in component_paths().items():
        digest_file = sha256()
        with path.open('rb') as stream:
            for block in iter(lambda: stream.read(1_048_576), b''):
                digest_file.update(block)
        if digest_file.hexdigest() != lock.components[name]:
            raise ValueError('GLARE_RUNTIME_BYTES_MISMATCH')
    return digest


def load_glare_donor_runtime_lock(path: Path,
                                  expected_sha: str) -> GlareDonorRuntimeLock:
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as stream:
            info = os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not 1 <= info.st_size <= 65536:
                raise ValueError('GLARE_RUNTIME_LOCK_MISMATCH')
            raw = stream.read(65537)
        if len(raw) != info.st_size:
            raise ValueError('GLARE_RUNTIME_LOCK_MISMATCH')
        parsed = parse_json(raw, max_bytes=65536)
        if canonical_bytes(parsed) != raw:
            raise ValueError('GLARE_RUNTIME_LOCK_MISMATCH')
        lock = GlareDonorRuntimeLock.model_validate_json(raw)
        verify_installed_runtime(lock, expected_sha)
        return lock
    except OSError as error:
        raise ValueError('GLARE_RUNTIME_LOCK_MISMATCH') from error

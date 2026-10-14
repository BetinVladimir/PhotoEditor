"""Region build identity including full Render code and installed Pillow extensions.

Model/ICC locks and native operator/CUDA qualification remain separate gates.
"""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
import sys
from typing import Literal
from pydantic import model_validator
from photo_worker.contracts.base import Contract, SHA256, canonical_hash, canonical_bytes
from photo_worker.runtime.executor import OperationFailure

REGION_COMPONENTS = ('adapter', 'admission', 'contracts', 'inputs', 'pixels',
    'presentation', 'resources', 'result', 'stage', 'runtime_lock', 'renderer', 'mapped', 'calibration')


def component_paths():
    from photo_worker.v2.render_runtime import component_paths as render_paths
    import PIL._imaging
    import PIL._imagingcms
    root = Path(__file__).resolve().parents[2]
    result = dict(render_paths())
    for name in REGION_COMPONENTS:
        result['region_' + name] = root / 'operations/region' / (name + '.py')
    for name, relative in {
        'region_adoption': 'v2/region_adoption.py',
        'icc_registry': 'color/icc.py',
    }.items():
        result[name] = root / relative
    result['pillow_imaging'] = Path(PIL._imaging.__file__)
    result['pillow_imagingcms'] = Path(PIL._imagingcms.__file__)
    return result


def installed_component_hashes():
    result = {}
    for name,path in component_paths().items():
        digest = sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(1048576),b''):
                digest.update(block)
        result[name] = digest.hexdigest()
    return result


class RegionRuntimeLock(Contract):
    schema_version: Literal[1]
    python_version: str
    implementation: Literal['CPython']
    machine: str
    packages: dict[str,str]
    components: dict[str,SHA256]

    @model_validator(mode='after')
    def inventory(self):
        import re
        if (re.fullmatch(r'\d+\.\d+\.\d+',self.python_version) is None
                or not 1<=len(self.machine)<=80
                or set(self.packages)!={'numpy','pydantic','rfc8785','Pillow'}
                or any(not 1<=len(version)<=120 for version in self.packages.values())
                or set(self.components)!=set(component_paths())):
            raise ValueError('REGION_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock: RegionRuntimeLock, expected_sha: str) -> str:
    lock = RegionRuntimeLock.model_validate_json(canonical_bytes(lock))
    digest = canonical_hash(lock)
    if digest != expected_sha:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation()!=lock.implementation
            or '.'.join(map(str,sys.version_info[:3]))!=lock.python_version
            or platform.machine()!=lock.machine):
        raise OperationFailure('RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name)!=version for name,version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    if installed_component_hashes()!=lock.components:
        raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    return digest


def load_region_runtime_lock(path, expected_sha: str) -> RegionRuntimeLock:
    """Read one bounded regular file and verify externally pinned build identity."""
    import os
    import stat
    from photo_worker.contracts.base import parse_json
    try:
        descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
        with os.fdopen(descriptor, 'rb') as source:
            info = os.fstat(source.fileno())
            if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1
                    or not 1 <= info.st_size <= 65536):
                raise OperationFailure('RUNTIME_LOCK_MISMATCH')
            raw = source.read(65537)
        if len(raw) != info.st_size:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        parsed = parse_json(raw, max_bytes=65536)
        if canonical_bytes(parsed) != raw:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        lock = RegionRuntimeLock.model_validate_json(raw)
    except (OSError, ValueError) as error:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH') from error
    verify_installed_runtime(lock, expected_sha)
    return lock

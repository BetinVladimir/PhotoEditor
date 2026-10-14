"""Independent execution pin for the OpenCV automatic-line detector.

Byte identity is not photographic qualification. Never reuse the correction
runtime lock: its dependency inventory does not include OpenCV.
"""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
import sys
from typing import Annotated, Literal

from pydantic import Field, model_validator
from photo_worker.contracts.base import Contract, SHA256, canonical_hash, canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from .perspective_runtime_lock import component_paths as correction_component_paths

PACKAGES = frozenset({'numpy', 'pydantic', 'rfc8785', 'opencv-python-headless'})
OPENCV_DISTRIBUTIONS = ('opencv-python', 'opencv-contrib-python', 'opencv-contrib-python-headless')


def component_paths():
    paths = dict(correction_component_paths())
    root = Path(__file__).resolve().parents[2]
    for name, relative in {
        'lines_algorithm': 'algorithms/perspective_lines.py',
        'lines_contracts': 'contracts/perspective_line_detection.py',
        'lines_producer': 'operations/optical/perspective_line_detection.py',
        'lines_evidence': 'operations/optical/perspective_line_evidence.py',
        'lines_resources': 'operations/optical/perspective_line_resources.py',
        'lines_adoption_recovery': 'v2/perspective_lines_adoption_recovery.py',
        'lines_adopter_config': 'v2/perspective_lines_adopter_config.py',
        'lines_adopter_schema': 'v2/perspective_lines_schema.py',
        'lines_adoption_commit': 'v2/perspective_lines_adoption_commit.py',
        'lines_adoption_prepare': 'v2/perspective_lines_adoption_prepare.py',
        'lines_adoption_evidence': 'v2/perspective_lines_adoption_evidence.py',
        'lines_replay': 'v2/perspective_lines_replay.py',
        'lines_validation': 'v2/perspective_lines_validation.py',
        'lines_config': 'runtime/perspective_lines_config.py',
        'lines_installation': 'operations/optical/perspective_lines_installation.py',
        'lines_runtime_lock': 'operations/optical/perspective_lines_runtime_lock.py',
    }.items():
        paths[name] = root / relative
    # Include bootstrap Python and all bundled native dependencies, not just
    # cv2.__file__ (which commonly points at Python rather than the extension).
    distribution = metadata.distribution('opencv-python-headless')
    files = distribution.files
    if not files:
        raise ValueError('PERSPECTIVE_LINES_OPENCV_INVENTORY_MISSING')
    native = False
    for entry in files:
        relative = str(entry).replace('\\', '/')
        if not (relative.startswith('cv2/') or relative.startswith(('opencv_python_headless.libs/', 'cv2/.dylibs/'))):
            continue
        if '..' in Path(relative).parts:
            raise ValueError('PERSPECTIVE_LINES_OPENCV_INVENTORY_INVALID')
        if relative.endswith(('.py', '.so', '.pyd', '.dll', '.dylib')) or '.so.' in relative:
            paths['opencv/' + relative] = Path(distribution.locate_file(entry))
            if relative.startswith('cv2/') and relative.endswith(('.so', '.pyd')):
                native = True
    if not native:
        raise ValueError('PERSPECTIVE_LINES_OPENCV_BINARY_MISSING')
    return paths


class PerspectiveLinesRuntimeLock(Contract):
    schema_version: Literal[1]
    python_version: Annotated[str, Field(pattern=r'^\d+\.\d+\.\d+$')]
    implementation: Literal['CPython']
    machine: Annotated[str, Field(min_length=1, max_length=80)]
    packages: dict[str, Annotated[str, Field(min_length=1, max_length=120)]]
    components: dict[str, SHA256]

    @model_validator(mode='after')
    def inventory(self):
        if set(self.packages) != PACKAGES:
            raise ValueError('PERSPECTIVE_LINES_RUNTIME_PACKAGES_INVALID')
        if set(self.components) != set(component_paths()):
            raise ValueError('PERSPECTIVE_LINES_RUNTIME_COMPONENTS_INVALID')
        return self


def installed_component_hashes():
    result = {}
    for name, path in component_paths().items():
        digest = sha256()
        with path.open('rb') as source:
            for block in iter(lambda: source.read(1024 * 1024), b''):
                digest.update(block)
        result[name] = digest.hexdigest()
    return result


def verify_opencv_origin():
    import cv2
    origin = Path(cv2.__file__).resolve()
    allowed = {path.resolve() for name, path in component_paths().items() if name.startswith('opencv/')}
    if origin not in allowed:
        raise OperationFailure('RUNTIME_BYTES_MISMATCH')


def verify_installed_runtime(lock: PerspectiveLinesRuntimeLock, expected_sha: str) -> str:
    # Revalidate model_copy/model_construct callers, too.
    try:
        lock = PerspectiveLinesRuntimeLock.model_validate_json(
            canonical_bytes(lock))
        digest = canonical_hash(lock)
        if digest != expected_sha:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        if (platform.python_implementation() != lock.implementation or
                '.'.join(map(str, sys.version_info[:3])) != lock.python_version or
                platform.machine() != lock.machine):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
        for name in OPENCV_DISTRIBUTIONS:
            try:
                metadata.version(name)
            except metadata.PackageNotFoundError:
                continue
            raise OperationFailure('RUNTIME_UNQUALIFIED')
        if any(metadata.version(name) != version for name, version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
        verify_opencv_origin()
        if installed_component_hashes() != lock.components:
            raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    except (ValueError, OSError, metadata.PackageNotFoundError) as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    return digest

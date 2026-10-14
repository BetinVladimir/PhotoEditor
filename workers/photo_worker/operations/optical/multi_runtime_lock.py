"""Installed CPU byte identity for V29 panorama and focus stack.

Includes all multi-input preparation, shared ORB/RANSAC, geometry, color,
contracts and OpenCV native dependencies. Byte identity is not qualification.
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
    from .perspective_lines_runtime_lock import component_paths as opencv_inventory
    paths = dict(correction_component_paths())
    # Reuse native distribution inventory, not detector qualification identity.
    paths.update({key:path for key,path in opencv_inventory().items() if key.startswith('opencv/')})
    root = Path(__file__).resolve().parents[2]
    relative_paths = ('algorithms/multi_registration.py', 'algorithms/multi_proxy.py', 'algorithms/multi_sampling.py', 'algorithms/multi_motion.py', 'algorithms/panorama_geometry.py', 'algorithms/panorama_blend.py', 'algorithms/focus_pyramid.py', 'algorithms/focus_motion.py', 'algorithms/semantic_transfer.py', 'algorithms/registration.py', 'algorithms/color.py', 'contracts/optical_multi.py', 'contracts/images.py', 'contracts/base.py', 'operations/session/contracts.py', 'runtime/scratch.py', 'runtime/executor.py', 'operations/optical/multi_runtime_lock.py', 'operations/optical/multi_admission.py', 'operations/optical/multi_admitted_prepare.py', 'operations/optical/multi_source_closure.py', 'operations/optical/multi_adapter.py', 'operations/optical/multi_evidence.py', 'operations/optical/multi_installation.py', 'runtime/arrays.py', 'runtime/multi_config.py', 'v2/multi_adoption.py', 'v2/multi_replay.py', 'v2/multi_adoption_evidence.py', 'v2/multi_adoption_prepare.py', 'v2/adoption.py', 'v2/adoption_service.py', 'v2/multi_adopter_config.py', 'v2/multi_adoption_schema.py', 'v2/service.py', 'runtime/service.py', 'cli.py', 'v2/array_evidence.py', 'operations/optical/multi_sources.py', 'operations/optical/multi_prepare.py', 'operations/optical/multi_image_prepare.py', 'operations/optical/multi_request.py', 'operations/optical/multi_result.py', 'operations/optical/multi_lineage.py', 'operations/optical/multi_encoding.py', 'operations/optical/registered_frame.py', 'operations/optical/panorama_prepare.py', 'operations/optical/focus_prepare.py', 'operations/optical/cylindrical_prepare.py', 'operations/optical/cylindrical_stack.py', 'operations/optical/cylindrical_validation.py', 'operations/optical/perspective_lines_runtime_lock.py')
    for relative in relative_paths:
        paths['multi/'+relative] = root / relative
    return paths


class MultiImageRuntimeLock(Contract):
    schema_version: Literal[1]
    python_version: Annotated[str, Field(pattern=r'^\d+\.\d+\.\d+$')]
    implementation: Literal['CPython']
    machine: Annotated[str, Field(min_length=1, max_length=80)]
    packages: dict[str, Annotated[str, Field(min_length=1, max_length=120)]]
    components: dict[str, SHA256]

    @model_validator(mode='after')
    def inventory(self):
        if set(self.packages) != PACKAGES:
            raise ValueError('MULTI_RUNTIME_PACKAGES_INVALID')
        if set(self.components) != set(component_paths()):
            raise ValueError('MULTI_RUNTIME_COMPONENTS_INVALID')
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


def verify_installed_runtime(lock: MultiImageRuntimeLock, expected_sha: str) -> str:
    # Revalidate model_copy/model_construct callers, too.
    try:
        lock = MultiImageRuntimeLock.model_validate_json(
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

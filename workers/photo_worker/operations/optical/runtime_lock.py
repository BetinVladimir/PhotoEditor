"""Verify an operator-pinned optical CPU runtime against installed distributions."""
from hashlib import sha256
from pathlib import Path
import platform
import sys
from importlib import metadata
from typing import Annotated,Literal
from pydantic import Field,model_validator
from photo_worker.contracts.base import Contract,canonical_hash,SHA256
from photo_worker.runtime.executor import OperationFailure


class OpticalRuntimeLock(Contract):
    schema_version:Literal[1]
    python_version:Annotated[str,Field(pattern=r'^\d+\.\d+\.\d+$')]
    implementation:Literal['CPython']
    machine:Annotated[str,Field(min_length=1,max_length=80)]
    packages:dict[str,Annotated[str,Field(min_length=1,max_length=120)]]

    components:dict[str,SHA256]

    @model_validator(mode='after')
    def required_packages(self):
        if set(self.packages)!={'numpy','pydantic','rfc8785'}:
            raise ValueError('OPTICAL_RUNTIME_PACKAGES_INVALID')
        if set(self.components)!=set(component_paths()):
            raise ValueError('OPTICAL_RUNTIME_COMPONENTS_INVALID')
        return self


def verify_installed_runtime(lock:OpticalRuntimeLock,expected_sha:str)->str:
    digest=canonical_hash(lock)
    if digest!=expected_sha:raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation()!=lock.implementation or
            '.'.join(map(str,sys.version_info[:3]))!=lock.python_version or
            platform.machine()!=lock.machine):
        raise OperationFailure('RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name)!=version for name,version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    actual=installed_component_hashes()
    if actual!=lock.components:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    return digest


def component_paths():
    import numpy as np
    import pydantic_core._pydantic_core as pydantic_binary
    import rfc8785._impl as canonical_implementation
    core=np._core if hasattr(np,'_core') else np.core
    root=Path(__file__).resolve().parents[2]
    return {'python':Path(sys.executable).resolve(),
            'numpy_core':Path(core._multiarray_umath.__file__),
            'pydantic_core':Path(pydantic_binary.__file__),
            'canonical_impl':Path(canonical_implementation.__file__),
            'lens_algorithm':root/'algorithms/lens.py',
            'lens_prepare':root/'operations/optical/prepare.py',
            'lens_color':root/'operations/optical/color.py',
            'lens_adapter':root/'operations/optical/adapter.py',
            'lens_result':root/'operations/optical/result.py',
            'lens_resources':root/'operations/optical/resources.py',
            'lens_admission':root/'operations/optical/admission.py',
            'lens_metadata_source':root/'operations/optical/metadata_source.py',
            'lens_exif':root/'operations/optical/exif.py'}


def installed_component_hashes():
    result={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(1024*1024),b''):digest.update(block)
        result[name]=digest.hexdigest()
    return result

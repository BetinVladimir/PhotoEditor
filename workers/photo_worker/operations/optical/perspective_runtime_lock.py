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


class PerspectiveRuntimeLock(Contract):
    schema_version:Literal[1]
    python_version:Annotated[str,Field(pattern=r'^\d+\.\d+\.\d+$')]
    implementation:Literal['CPython']
    machine:Annotated[str,Field(min_length=1,max_length=80)]
    packages:dict[str,Annotated[str,Field(min_length=1,max_length=120)]]

    components:dict[str,SHA256]

    @model_validator(mode='after')
    def required_packages(self):
        if set(self.packages)!={'numpy','pydantic','rfc8785'}:
            raise ValueError('PERSPECTIVE_RUNTIME_PACKAGES_INVALID')
        if set(self.components)!=set(component_paths()):
            raise ValueError('PERSPECTIVE_RUNTIME_COMPONENTS_INVALID')
        return self


def verify_installed_runtime(lock:PerspectiveRuntimeLock,expected_sha:str)->str:
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
            'perspective_algorithm':root/'algorithms/perspective.py',
            'perspective_geometry':root/'operations/optical/perspective_geometry.py',
            'perspective_prepare':root/'operations/optical/perspective_prepare.py',
            'perspective_color':root/'operations/optical/color.py',
            'perspective_icc':root/'operations/optical/perspective_color.py',
            'perspective_source':root/'operations/optical/perspective_source.py',
            'icc_registry':root/'color/icc.py',
            'perspective_alpha':root/'operations/optical/alpha.py',
            'perspective_adapter':root/'operations/optical/perspective_adapter.py',
            'perspective_result':root/'operations/optical/perspective_result.py',
            'perspective_resources':root/'operations/optical/perspective_resources.py',
            'perspective_admission':root/'operations/optical/perspective_admission.py',
            'perspective_contracts':root/'contracts/optical_perspective.py',
            'perspective_adopter_config':root/'v2/perspective_adopter_config.py',
            'perspective_adoption_prepare':root/'v2/perspective_adoption_prepare.py',
            'perspective_adoption_evidence':root/'v2/perspective_adoption_evidence.py',
            'perspective_adoption':root/'v2/perspective_adoption.py',
            'perspective_config':root/'runtime/perspective_config.py',
            'perspective_installation':root/'operations/optical/perspective_installation.py',
            'perspective_evidence':root/'operations/optical/perspective_evidence.py',
            'perspective_runtime_lock':root/'operations/optical/perspective_runtime_lock.py',
            'qualification_report':root/'v2/semantic_qualification_report.py',
            'qualification_pin':root/'v2/semantic_qualification_pin.py',
            'cpu_qualification_pin':root/'v2/negative_qualification.py'}


def installed_component_hashes():
    result={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(1024*1024),b''):digest.update(block)
        result[name]=digest.hexdigest()
    return result

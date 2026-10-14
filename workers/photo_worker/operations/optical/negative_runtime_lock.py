"""Verify an operator-pinned negative CPU runtime against installed distributions."""
from hashlib import sha256
from pathlib import Path
import platform
import sys
from importlib import metadata
from typing import Annotated,Literal
from pydantic import Field,model_validator
from photo_worker.contracts.base import Contract,canonical_hash,SHA256
from photo_worker.runtime.executor import OperationFailure


class NegativeRuntimeLock(Contract):
    schema_version:Literal[1]
    epsilon:Annotated[float,Field(gt=0,le=1e-6)]
    python_version:Annotated[str,Field(pattern=r'^\d+\.\d+\.\d+$')]
    implementation:Literal['CPython']
    machine:Annotated[str,Field(min_length=1,max_length=80)]
    packages:dict[str,Annotated[str,Field(min_length=1,max_length=120)]]

    components:dict[str,SHA256]

    @model_validator(mode='after')
    def required_packages(self):
        if set(self.packages)!={'numpy','pydantic','rfc8785'}:
            raise ValueError('NEGATIVE_RUNTIME_PACKAGES_INVALID')
        if set(self.components)!=set(component_paths()):
            raise ValueError('NEGATIVE_RUNTIME_COMPONENTS_INVALID')
        return self


def verify_installed_runtime(lock:NegativeRuntimeLock,expected_sha:str)->str:
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
            'negative_params':root/'contracts/optical_negative.py',
            'negative_algorithm':root/'operations/optical/negative.py',
            'negative_calibration':root/'operations/optical/negative_calibration.py',
            'negative_frame':root/'operations/optical/negative_frame.py',
            'negative_changes':root/'operations/optical/negative_changes.py',
            'negative_prepare':root/'operations/optical/negative_prepare.py',
            'negative_result':root/'operations/optical/negative_result.py',
            'negative_statistics':root/'operations/optical/negative_statistics.py',
            'negative_stage':root/'operations/optical/negative_stage.py',
            'negative_runtime':root/'operations/optical/negative_runtime_lock.py',
            'negative_resources':root/'operations/optical/negative_resources.py',
            'negative_leased_stage':root/'v2/negative_leased_stage.py',
            'negative_leased_prepare':root/'v2/negative_leased_prepare.py',
            'negative_source':root/'v2/negative_source.py',
            'negative_attempt':root/'v2/negative_attempt.py',
            'negative_adoption':root/'v2/negative_adoption.py',
            'negative_qualification':root/'v2/negative_qualification.py',
            'negative_qualification_catalog':root/'v2/negative_qualification_catalog.py',
            'negative_color':root/'operations/optical/negative_color.py',
            'negative_icc':root.parent.parent/'resources/icc/linear_srgb_v1.icc',
            'negative_adapter':root/'operations/optical/negative_adapter.py',
            'negative_execute':root/'v2/negative_execute.py',
            'negative_reader':root/'v2/negative_reader.py',
            'negative_handoff':root/'operations/optical/negative_handoff.py',
            'negative_object_reader':root/'v2/negative_object_reader.py',
            'negative_qualification_loader':root/'v2/negative_qualification_loader.py',
            'negative_qualification_objects':root/'v2/negative_qualification_objects.py',
            'negative_qualified_prepare':root/'v2/negative_qualified_prepare.py',
            'qualification_report':root/'v2/semantic_qualification_report.py',
            'qualification_pin':root/'v2/semantic_qualification_pin.py',
            'array_evidence':root/'v2/array_evidence.py',
            'image_contracts':root/'contracts/images.py',
            'optical_contracts':root/'operations/optical/contracts.py',
            'session_contracts':root/'operations/session/contracts.py',
            'base_contracts':root/'contracts/base.py'}


def installed_component_hashes():
    result={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(1024*1024),b''):digest.update(block)
        result[name]=digest.hexdigest()
    return result


def load_negative_runtime_lock(path:Path,expected_sha:str)->NegativeRuntimeLock:
    """Read operator-pinned regular canonical bytes without following links."""
    import os
    import stat
    import re
    from photo_worker.contracts.base import canonical_bytes,parse_json
    if not isinstance(expected_sha,str) or re.fullmatch('[0-9a-f]{64}',expected_sha) is None:
        raise ValueError('NEGATIVE_RUNTIME_PIN_INVALID')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(fd,'rb') as stream:
        info=os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 2<=info.st_size<=65536:
            raise ValueError('NEGATIVE_RUNTIME_FILE_INVALID')
        raw=stream.read(65537)
    if len(raw)!=info.st_size or sha256(raw).hexdigest()!=expected_sha:
        raise ValueError('NEGATIVE_RUNTIME_FILE_HASH_MISMATCH')
    parsed=parse_json(raw,65536)
    lock=NegativeRuntimeLock.model_validate_json(canonical_bytes(parsed))
    if canonical_bytes(lock)!=raw:raise ValueError('NEGATIVE_RUNTIME_FILE_NONCANONICAL')
    verify_installed_runtime(lock,expected_sha)
    return lock

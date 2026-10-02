"""Operator-pinned CPU composition build identity; never self-qualifies a build."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
import sys
from typing import Annotated, Literal
from pydantic import Field, model_validator
from photo_worker.contracts.base import Contract, SHA256, canonical_hash
from photo_worker.runtime.executor import OperationFailure


def component_paths():
    from photo_worker.operations.optical.runtime_lock import component_paths as optical_paths
    binary = optical_paths()
    root = Path(__file__).resolve().parents[2]
    paths = {name: binary[name] for name in ('python','numpy_core','pydantic_core','canonical_impl')}
    for name in ('layer_compose','layer_masks','layer_lineage'):
        paths[name] = root/'algorithms'/(name+'.py')
    for name in ('adapter','admission','alpha','contracts','inputs','mask_inputs','parents',
                 'prepare','reports','resources','result','runtime_lock','output_validation'):
        paths['layer_'+name] = root/'operations/layers'/(name+'.py')
    for name in ('base','images'):
        paths['contract_'+name] = root/'contracts'/(name+'.py')
    for name in ('arrays','artifacts','database','executor','parameters','scratch'):
        paths['runtime_'+name] = root/'runtime'/(name+'.py')
    for name,relative in {
        'optical_color':'operations/optical/color.py',
        'optical_alpha':'operations/optical/alpha.py',
        'optical_contracts':'operations/optical/contracts.py',
        'optical_runtime_lock':'operations/optical/runtime_lock.py',
        'session_contracts':'operations/session/contracts.py',
        'input_closure':'v2/input_closure.py',
        'render_reports':'v2/render_reports.py',
        'wire_contracts':'contracts/wire.py',
    }.items():
        paths[name] = root/relative
    return paths


class LayerRuntimeLock(Contract):
    schema_version: Literal[1]
    python_version: Annotated[str,Field(pattern=r'^\d+\.\d+\.\d+$')]
    implementation: Literal['CPython']
    machine: Annotated[str,Field(min_length=1,max_length=80)]
    packages: dict[str,Annotated[str,Field(min_length=1,max_length=120)]]
    components: dict[str,SHA256]

    @model_validator(mode='after')
    def required_components(self):
        if set(self.packages) != {'numpy','pydantic','rfc8785'}:
            raise ValueError('LAYER_RUNTIME_PACKAGES_INVALID')
        if set(self.components) != set(component_paths()):
            raise ValueError('LAYER_RUNTIME_COMPONENTS_INVALID')
        return self


def installed_component_hashes():
    result = {}
    for name,path in component_paths().items():
        digest = sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(1024*1024),b''):
                digest.update(block)
        result[name] = digest.hexdigest()
    return result


def verify_installed_runtime(lock: LayerRuntimeLock, expected_sha: str) -> str:
    digest = canonical_hash(lock)
    if digest != expected_sha:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation() != lock.implementation
            or '.'.join(map(str,sys.version_info[:3])) != lock.python_version
            or platform.machine() != lock.machine):
        raise OperationFailure('RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name) != version for name,version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    if installed_component_hashes() != lock.components:
        raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    return digest


def load_layer_runtime_lock(path, expected_sha: str) -> LayerRuntimeLock:
    """Read one bounded regular file without following links or reopening it."""
    import os
    import stat
    from photo_worker.contracts.base import parse_json
    descriptor = os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(descriptor,'rb') as source:
        info=os.fstat(source.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 1<=info.st_size<=65536:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        raw=source.read(65537)
    if len(raw)!=info.st_size:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    try:
        parse_json(raw,max_bytes=65536)
        lock=LayerRuntimeLock.model_validate_json(raw)
    except ValueError as error:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH') from error
    verify_installed_runtime(lock,expected_sha)
    return lock

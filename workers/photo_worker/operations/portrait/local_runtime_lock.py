"""V28 CPU build identity; quality/host qualification remains separate."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
import sys
import re
from typing import Literal
from pydantic import model_validator
from photo_worker.contracts.base import Contract,SHA256,canonical_hash,canonical_bytes
from photo_worker.runtime.executor import OperationFailure


def component_paths():
    import cv2
    from photo_worker.operations.optical.runtime_lock import component_paths as optical_paths
    base=optical_paths()
    result={name:base[name] for name in ('python','numpy_core','pydantic_core','canonical_impl')}
    root=Path(__file__).resolve().parents[2]
    binaries=[p for p in Path(cv2.__file__).parent.glob('cv2.*') if p.suffix in ('.so','.pyd','.dylib')]
    if len(binaries)!=1:raise OperationFailure('RUNTIME_UNQUALIFIED')
    result['opencv_binary']=binaries[0]
    for name,relative in {'local_algorithm':'algorithms/portrait_local.py','local_tiles':'algorithms/portrait_local_tiles.py',
        'local_contracts':'contracts/portrait_local.py','manual_contracts':'contracts/manual_mask.py','image_contracts':'contracts/images.py','base_contracts':'contracts/base.py',
        'manual_raster':'algorithms/manual_mask.py','local_color':'operations/optical/color.py',
        'runtime_executor':'runtime/executor.py','runtime_database':'runtime/database.py','runtime_arrays':'runtime/arrays.py','runtime_artifacts':'runtime/artifacts.py','runtime_scratch':'runtime/scratch.py'}.items():
        result[name]=root/relative
    for name in ('local_adapter','local_inputs','local_resources','prepare_local','local_result',
                 'local_manual_inputs','local_manual_proof','local_manual_admission',
                 'local_sql_origin','local_runtime_lock'):
        result[name]=root/'operations/portrait'/(name+'.py')
    result['local_portrait_config']=root/'runtime/local_portrait_config.py'
    return result


def installed_component_hashes():
    result={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as stream:
            for block in iter(lambda:stream.read(1048576),b''):digest.update(block)
        result[name]=digest.hexdigest()
    return result


class LocalPortraitRuntimeLock(Contract):
    schema_version:Literal[1]
    python_version:str
    implementation:Literal['CPython']
    machine:str
    packages:dict[str,str]
    components:dict[str,SHA256]

    @model_validator(mode='after')
    def inventory(self):
        if (re.fullmatch(r'\d+\.\d+\.\d+',self.python_version) is None
                or not 1<=len(self.machine)<=80
                or set(self.packages)!={'numpy','pydantic','rfc8785','opencv-python-headless'}
                or any(not 1<=len(v)<=120 for v in self.packages.values())
                or set(self.components)!=set(component_paths())):
            raise ValueError('PORTRAIT_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock,expected_sha):
    lock=LocalPortraitRuntimeLock.model_validate_json(canonical_bytes(lock))
    digest=canonical_hash(lock)
    if digest!=expected_sha:raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation()!=lock.implementation
            or '.'.join(map(str,sys.version_info[:3]))!=lock.python_version
            or platform.machine()!=lock.machine):raise OperationFailure('RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name)!=version for name,version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    if installed_component_hashes()!=lock.components:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    return digest



def load_local_portrait_runtime_lock(path, expected_sha):
    """Read one bounded regular canonical lock file with an external SHA pin."""
    import os
    import stat
    from photo_worker.contracts.base import parse_json
    try:
        descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(descriptor,'rb') as stream:
            info=os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 1<=info.st_size<=65536:
                raise OperationFailure('RUNTIME_LOCK_MISMATCH')
            raw=stream.read(65537)
        if len(raw)!=info.st_size:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        parsed=parse_json(raw,max_bytes=65536)
        if canonical_bytes(parsed)!=raw:
            raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        lock=LocalPortraitRuntimeLock.model_validate_json(raw)
    except (OSError,ValueError) as error:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH') from error
    verify_installed_runtime(lock,expected_sha)
    return lock

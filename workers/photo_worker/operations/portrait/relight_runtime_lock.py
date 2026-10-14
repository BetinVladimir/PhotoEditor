"""V28 relight CPU build identity; quality/host qualification remains separate."""
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
    from .local_runtime_lock import component_paths as local_paths
    base=local_paths()
    names=('python','numpy_core','pydantic_core','canonical_impl','opencv_binary',
        'local_algorithm','local_color','image_contracts','base_contracts',
        'runtime_executor','runtime_database','runtime_arrays','runtime_artifacts','runtime_scratch')
    result={name:base[name] for name in names}
    root=Path(__file__).resolve().parents[2]
    paths={'relight_worker_config':'runtime/relight_config.py','worker_service':'runtime/service.py','worker_cli':'cli.py',
        'relight_algorithm':'algorithms/relight.py','relight_contracts':'contracts/relight.py',
        'result_contracts':'operations/optical/contracts.py',
        'runtime_gateway':'runtime/gateway.py','runtime_parameters':'runtime/parameters.py',
        'session_contracts':'operations/session/contracts.py',
        'relight_qualification':'v2/relight_qualification.py',
        'relight_qualification_loader':'v2/relight_qualification_loader.py',
        'depth_contracts':'contracts/depth.py',
        'person_contracts':'contracts/person.py',
        'manual_contracts':'contracts/manual_mask.py',
        'manual_algorithm':'algorithms/manual_mask.py',
        'manual_raster_verify':'operations/mask/verify.py',
        'qualification_pin':'v2/semantic_qualification_pin.py',
        'qualification_report':'v2/semantic_qualification_report.py',
        'adoption_contracts':'v2/adoption.py'}
    for name in ('relight_adoption','relight_adoption_source','relight_adoption_depth',
                 'relight_adoption_person','relight_adoption_manual','relight_adoption_authority',
                 'relight_adopter_config','service','relight_adoption_qualification','relight_adoption_prepare','adoption_service','adoption_store','relight_replay','array_evidence'):
        paths[name]='v2/'+name+'.py'
    for name in ('relight_adapter','relight_context','relight_inputs','prepare_relight','relight_result',
                 'relight_runtime_lock','relight_resolver','relight_admission','relight_depth_authority','relight_person_authority','relight_manual_authority','local_manual_inputs','local_manual_proof','local_manual_admission','local_resources'):
        paths[name]='operations/portrait/'+name+'.py'
    result.update({name:root/relative for name,relative in paths.items()})
    return result


def installed_component_hashes():
    result={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as stream:
            for block in iter(lambda:stream.read(1048576),b''):digest.update(block)
        result[name]=digest.hexdigest()
    return result


class RelightRuntimeLock(Contract):
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
            raise ValueError('RELIGHT_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock,expected_sha):
    lock=RelightRuntimeLock.model_validate_json(canonical_bytes(lock))
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



def load_relight_runtime_lock(path, expected_sha):
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
        lock=RelightRuntimeLock.model_validate_json(raw)
    except (OSError,ValueError) as error:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH') from error
    verify_installed_runtime(lock,expected_sha)
    return lock

"""Donor CPU implementation identity, separate from quality qualification."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
import re
import sys
from typing import Literal
from pydantic import model_validator
from photo_worker.contracts.base import Contract,SHA256,canonical_bytes,canonical_hash
from photo_worker.runtime.executor import OperationFailure
from .local_runtime_lock import component_paths as local_paths


def component_paths():
    paths=local_paths();root=Path(__file__).resolve().parents[2]
    for name in ('portrait_donor','portrait_donor_patch'):
        paths[name]=root/'algorithms'/(name+'.py')
    for name in ('portrait_open_eyes','donor_confirmation'):
        paths[name]=root/'contracts'/(name+'.py')
    for name in ('portrait_donor_images','portrait_donor_material','portrait_donor_masks',
            'portrait_donor_manual','portrait_donor_mask_patch','portrait_donor_evidence',
            'portrait_open_eyes_prepare','portrait_open_eyes_material','portrait_open_eyes_stage',
            'portrait_open_eyes_manual_stage','portrait_open_eyes_execute',
            'portrait_donor_input_documents','portrait_donor_reader','portrait_donor_object_reader',
            'portrait_donor_attempt','portrait_donor_source','portrait_donor_manual_authority',
            'portrait_donor_qualification','portrait_donor_qualification_loader',
            'portrait_donor_adoption','portrait_donor_replay',
            'portrait_donor_adoption_evidence','portrait_donor_adoption_qualification',
            'portrait_donor_adoption_manual','portrait_donor_adoption_prepare','portrait_donor_adopter_config',
            'render_replay_reader','adoption_store','adoption_service','service',
            'observation_evidence','array_evidence','adoption'):
        paths[name]=root/'v2'/(name+'.py')
    for name in ('donor_resources','donor_files','donor_result','donor_handoff','donor_adapter','donor_runtime_lock'):
        paths[name]=root/'operations/portrait'/(name+'.py')
    for name in ('database','donor_config','semantic_objects','artifacts'):
        paths['runtime_'+name]=root/'runtime'/(name+'.py')
    paths['session_contracts']=root/'operations/session/contracts.py'
    paths['raster_verify']=root/'operations/mask/verify.py'
    paths['optical_contracts']=root/'operations/optical/contracts.py'
    return paths


def installed_component_hashes():
    hashes={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as stream:
            for block in iter(lambda:stream.read(1_048_576),b''):digest.update(block)
        hashes[name]=digest.hexdigest()
    return hashes


class DonorPortraitRuntimeLock(Contract):
    schema_version:Literal[1]
    python_version:str
    implementation:Literal['CPython']
    machine:str
    packages:dict[str,str]
    components:dict[str,SHA256]

    @model_validator(mode='after')
    def inventory(self):
        if (re.fullmatch(r'\d+\.\d+\.\d+',self.python_version) is None or
                not 1<=len(self.machine)<=80 or
                set(self.packages)!={'numpy','pydantic','rfc8785','opencv-python-headless'} or
                any(not 1<=len(v)<=120 for v in self.packages.values()) or
                set(self.components)!=set(component_paths())):
            raise ValueError('DONOR_RUNTIME_INVENTORY_INVALID')
        return self


def verify_installed_runtime(lock,expected_sha):
    lock=DonorPortraitRuntimeLock.model_validate_json(canonical_bytes(lock))
    digest=canonical_hash(lock)
    if digest!=expected_sha:raise OperationFailure('RUNTIME_LOCK_MISMATCH')
    if (platform.python_implementation()!=lock.implementation or
            '.'.join(map(str,sys.version_info[:3]))!=lock.python_version or
            platform.machine()!=lock.machine):raise OperationFailure('RUNTIME_UNQUALIFIED')
    try:
        if any(metadata.version(name)!=version for name,version in lock.packages.items()):
            raise OperationFailure('RUNTIME_UNQUALIFIED')
    except metadata.PackageNotFoundError as error:
        raise OperationFailure('RUNTIME_UNQUALIFIED') from error
    if installed_component_hashes()!=lock.components:raise OperationFailure('RUNTIME_BYTES_MISMATCH')
    return digest


def load_donor_portrait_runtime_lock(path,expected_sha):
    """Bounded canonical lock from one regular non-symlink file, externally pinned."""
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
        if len(raw)!=info.st_size:raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        parsed=parse_json(raw,max_bytes=65536)
        if canonical_bytes(parsed)!=raw:raise OperationFailure('RUNTIME_LOCK_MISMATCH')
        lock=DonorPortraitRuntimeLock.model_validate_json(raw)
    except (OSError,ValueError) as error:
        raise OperationFailure('RUNTIME_LOCK_MISMATCH') from error
    verify_installed_runtime(lock,expected_sha)
    return lock

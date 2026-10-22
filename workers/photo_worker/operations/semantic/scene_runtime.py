"""Measured semantic scene runtime identity; no report generation or qualification."""
from hashlib import sha256
from pathlib import Path
from importlib import metadata
import platform
import sys
from photo_worker.contracts.base import Contract,SHA256,canonical_bytes,canonical_hash
from typing import Literal
from pydantic import model_validator


def component_paths():
    import cv2
    root=Path(__file__).resolve().parents[2]
    relatives=('algorithms/semantic_transfer.py','algorithms/semantic_scene.py',
        'algorithms/semantic_scene_projection.py','algorithms/registration.py',
        'operations/semantic/adapter.py','operations/semantic/transfer.py',
        'operations/semantic/prepare_transfer.py','operations/semantic/load_inputs.py',
        'operations/semantic/stage_transfer.py','operations/semantic/scene_runtime.py',
        'operations/semantic/scene_documents.py','operations/semantic/scene_worker.py',
        'operations/semantic/scene_refusal.py',
        'runtime/semantic_scene_config.py','runtime/semantic_scene_objects.py',
        'v2/semantic_scene_config.py','v2/semantic_scene_adoption.py',
        'operations/optical/color.py',
        'contracts/semantic.py','contracts/semantic_scene.py',
        'v2/semantic_transfer_authority.py','v2/semantic_replay.py',
        'v2/semantic_replay_inputs.py','v2/array_evidence.py',
        'v2/semantic_qualification_loader.py','v2/semantic_qualification_report.py')
    result={name:root/name for name in relatives}
    binaries=sorted(Path(cv2.__file__).resolve().parent.glob('cv2*.so'))
    if len(binaries)!=1:raise ValueError('SEMANTIC_OPENCV_BINARY_INVENTORY_INVALID')
    result['opencv_native']=binaries[0]
    return result


def installed_components(*,check_active=lambda:None):
    result={}
    for name,path in component_paths().items():
        check_active();digest=sha256()
        with path.open('rb') as source:
            while True:
                check_active();block=source.read(65536)
                if not block:break
                digest.update(block)
        result[name]=digest.hexdigest()
    check_active();return result


class SemanticSceneRuntime(Contract):
    schema_version:Literal[1]
    algorithm:Literal['farneback_depth_plane_v1']
    python_version:str
    implementation:Literal['CPython']
    machine:str
    packages:dict[str,str]
    components:dict[str,SHA256]

    @model_validator(mode='after')
    def exact_inventory(self):
        if (set(self.packages)!={'numpy','pydantic','rfc8785','opencv-python-headless'}
                or set(self.components)!=set(component_paths())
                or any(not 1<=len(v)<=120 for v in self.packages.values())
                or not 1<=len(self.machine)<=80):
            raise ValueError('SEMANTIC_SCENE_RUNTIME_INVALID')
        return self


def verify_scene_runtime(lock,expected_sha,*,check_active=lambda:None):
    check_active();lock=SemanticSceneRuntime.model_validate_json(lock if isinstance(lock,(bytes,str)) else canonical_bytes(lock))
    if (canonical_hash(lock)!=expected_sha or platform.python_implementation()!=lock.implementation
            or '.'.join(map(str,sys.version_info[:3]))!=lock.python_version or platform.machine()!=lock.machine
            or any(metadata.version(name)!=version for name,version in lock.packages.items())
            or installed_components(check_active=check_active)!=lock.components):
        raise ValueError('SEMANTIC_SCENE_RUNTIME_MISMATCH')
    check_active();return expected_sha

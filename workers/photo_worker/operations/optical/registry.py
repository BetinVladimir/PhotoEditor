"""Bounded immutable operator lens registry loading; no automatic qualification."""
import os
import stat
from hashlib import sha256
from typing import Annotated,Literal
from pydantic import Field,model_validator
from photo_worker.contracts.base import ArtifactRef,Contract,SHA256,canonical_hash,parse_json
from photo_worker.runtime.executor import OperationFailure
from .runtime_lock import OpticalRuntimeLock,verify_installed_runtime
from .admission import QualifiedLensPin

MAX_REGISTRY_BYTES=1024*1024


class LensRegistryPin(Contract):
    qualification:ArtifactRef
    coefficients_sha:SHA256
    runtime_lock_sha:SHA256
    dataset_sha:SHA256


class LensRegistry(Contract):
    schema_version:Literal[1]
    runtime:OpticalRuntimeLock
    profiles:Annotated[list[LensRegistryPin],Field(max_length=10000)]

    @model_validator(mode='after')
    def coherent_pins(self):
        runtime_sha=canonical_hash(self.runtime)
        seen=set()
        for pin in self.profiles:
            key=(pin.qualification.artifact_id,pin.qualification.sha256,pin.coefficients_sha)
            if key in seen or pin.runtime_lock_sha!=runtime_sha:
                raise ValueError('LENS_REGISTRY_PIN_INVALID')
            seen.add(key)
        return self

    def admission_pins(self):
        return tuple(QualifiedLensPin(pin.qualification,pin.coefficients_sha,pin.runtime_lock_sha,pin.dataset_sha)
                     for pin in self.profiles)


def load_lens_registry(path,expected_sha):
    """Expected raw-byte SHA must come from operator configuration, never a recipe."""
    descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    with os.fdopen(descriptor,'rb') as stream:
        info=os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 1<=info.st_size<=MAX_REGISTRY_BYTES:
            raise OperationFailure('LENS_REGISTRY_INVALID')
        raw=stream.read(MAX_REGISTRY_BYTES+1)
    if len(raw)!=info.st_size or sha256(raw).hexdigest()!=expected_sha:
        raise OperationFailure('LENS_REGISTRY_BYTES_MISMATCH')
    try:
        parse_json(raw,MAX_REGISTRY_BYTES)
        registry=LensRegistry.model_validate_json(raw)
    except ValueError as error:raise OperationFailure('LENS_REGISTRY_INVALID') from error
    verify_installed_runtime(registry.runtime,canonical_hash(registry.runtime))
    return registry

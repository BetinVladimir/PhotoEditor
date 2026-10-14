"""Prepare qualified negative replay outside the final adoption transaction."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.contracts.images import WorkingImage
from .adoption import AdoptionInvalid
from .lens_bypass import _document,_ref
from .negative_adoption_evidence import load_negative_adoption_evidence,negative_adoption_checkpoint
from .negative_qualification import NegativeQualificationPin
from .negative_qualified_replay import verify_qualified_negative_replay


@dataclass(frozen=True)
class NegativeAdoptionInstallation:
    policy: dict
    options: object
    scratch_root: Path
    max_bytes: int
    verify_color: object


@dataclass(frozen=True)
class PreparedNegativeReconstruction:
    evidence_jcs: bytes
    params_raw: bytes
    params_ref: object
    result_ref: object


def prepare_negative_reconstruction(connection,reader,tenant_id,task_id,context,params_ref,*,
        policy,options,scratch_root,max_bytes,verify_color,check_active):
    from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions
    if not isinstance(options,NegativeRuntimeOptions):raise ValueError('NEGATIVE_ADOPTION_CONFIGURATION')
    ctx=deepcopy(context);ref=deepcopy(params_ref);owned=deepcopy(options)
    owned_policy=deepcopy(policy)
    check_active()
    params=_document(reader,ref,OpticalNegativeParams,'input')
    raw=canonical_bytes(params)
    evidence=load_negative_adoption_evidence(connection,tenant_id,task_id,ctx,raw,check_active=check_active)
    if canonical_bytes(evidence['params_ref'])!=canonical_bytes(dict(artifact_id=str(ref.artifact_id),sha256=ref.sha256)):
        raise AdoptionInvalid('NEGATIVE_ADOPTION_PARAMS_CHANGED')
    checkpoint=negative_adoption_checkpoint(connection,tenant_id,task_id,ctx,raw,evidence,check_active=check_active)
    source=_document(reader,_ref(params.base.source),WorkingImage,'input')
    source_raw=canonical_bytes(source)
    def current_pin():
        return NegativeQualificationPin.from_value(checkpoint()['profile']['negative_qualification'])
    def source_authority(value,body):
        checkpoint()
        if canonical_bytes(value)!=raw or body!=source_raw:
            raise AdoptionInvalid('NEGATIVE_ADOPTION_SOURCE_CHANGED')
        checkpoint();return True
    verify_qualified_negative_replay(reader,ref,ctx.root,profile=evidence['profile'],policy=owned_policy,
        runtime_lock=owned.runtime_lock,runtime_sha=owned.runtime_sha,scratch_root=scratch_root,
        max_bytes=max_bytes,ram_max_bytes=owned.ram_max_bytes,current_pin=current_pin,
        source_authority=source_authority,verify_color=verify_color,check_active=check_active)
    checkpoint()
    return PreparedNegativeReconstruction(canonical_bytes(evidence),raw,ref,deepcopy(ctx.root))

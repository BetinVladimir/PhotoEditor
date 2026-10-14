"""Join current completed-task SQL evidence with qualified donor reconstruction."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes,ArtifactRef
from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
from .adoption import AdoptionInvalid,ArtifactRef as EvidenceRef
from .lens_bypass import _ref
from .portrait_donor_adoption_evidence import load_donor_adoption_evidence,donor_adoption_checkpoint
from .portrait_donor_adoption_qualification import load_donor_adoption_qualification
from .portrait_donor_adoption_manual import donor_adoption_manual_authority
from .portrait_donor_replay import verify_donor_reconstruction


@dataclass(frozen=True)
class DonorAdoptionInstallation:
    policy: dict
    options: DonorRuntimeOptions
    output_scratch_root: Path
    output_max_bytes: int
    ram_max_bytes: int


@dataclass(frozen=True)
class PreparedDonorReconstruction:
    evidence_jcs: bytes
    params_ref: EvidenceRef
    result_ref: EvidenceRef


def prepare_donor_reconstruction(connection,objects,reader,tenant_id,task_id,context,*,
        bucket,policy,options,output_scratch_root,output_max_bytes,ram_max_bytes,check_active):
    """Prepare only; commit must independently re-lock current SQL evidence.

    Calibration threshold comes from verified original qualification objects.
    Caller may not inject source/manual authority or override that threshold.
    """
    if not isinstance(options,DonorRuntimeOptions) or not callable(check_active):
        raise ValueError('DONOR_ADOPTION_CONFIGURATION')
    ctx=deepcopy(context);owned=deepcopy(options)
    evidence=load_donor_adoption_evidence(connection,tenant_id,task_id,ctx,check_active=check_active)
    checkpoint=donor_adoption_checkpoint(connection,tenant_id,task_id,ctx,evidence,check_active=check_active)
    pin,calibration=load_donor_adoption_qualification(evidence,objects,policy,bucket=bucket,
        checkpoint=checkpoint,check_active=check_active)
    if pin.runtime_lock_sha!=owned.runtime_sha:
        raise AdoptionInvalid('DONOR_ADOPTION_RUNTIME_CHANGED')
    manual=donor_adoption_manual_authority(connection,tenant_id,task_id,ctx,evidence,
        checkpoint=checkpoint,check_active=check_active)
    params_body=canonical_bytes(evidence['params']);proof=evidence['proof_jcs'].encode()
    def current_source(params,current_proof):
        checkpoint()
        if canonical_bytes(params)!=params_body or current_proof!=proof:
            raise AdoptionInvalid('DONOR_ADOPTION_SOURCE_CHANGED')
        checkpoint();return True
    def active():checkpoint()
    def ref(value):return ArtifactRef.model_validate_json(canonical_bytes(value))
    params_ref=ref(evidence['params_ref'])
    verify_donor_reconstruction(reader,_ref(params_ref),ctx.root,
        manual_root_ref=ref(evidence['manual']['root']),producer_ref=ref(evidence['manual']['original_params']),
        resource_profile=evidence['profile'],ram_max_bytes=ram_max_bytes,
        min_face_edge_px=calibration.min_face_edge_px,runtime_lock=owned.runtime_lock,runtime_sha=owned.runtime_sha,
        mask_scratch_root=owned.mask_scratch_root,mask_max_bytes=owned.mask_max_bytes,
        material_scratch_root=owned.material_scratch_root,material_max_bytes=owned.material_max_bytes,
        output_scratch_root=output_scratch_root,output_max_bytes=output_max_bytes,
        authority=current_source,producer_authority=manual,check_active=active,alpha_mode=owned.alpha_mode)
    checkpoint()
    return PreparedDonorReconstruction(canonical_bytes(evidence),_ref(params_ref),deepcopy(ctx.root))

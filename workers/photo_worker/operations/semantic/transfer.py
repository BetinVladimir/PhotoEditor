"""Explicitly installed V27 transfer worker; qualification/adoption are separate."""
from dataclasses import dataclass
from copy import deepcopy
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import SemanticTransferParams,EditIntent,bind_semantic_transfer_documents
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.executor import OperationResult,OperationFailure
from .prepare_transfer import prepare_calibrated_planar_transfer
from .stage_transfer import stage_planar_transfer
from .resources import semantic_transfer_resources


@dataclass(frozen=True)
class SemanticTransferArrays:
    reference: np.ndarray
    target: np.ndarray
    mask: np.ndarray
    reference_protected: np.ndarray
    target_protected: np.ndarray


def execute_transfer(ctx,artifacts,*,load_arrays,verify_runtime,verify_scene):
    """Qualified installer supplies scoped arrays and complete scene verifier.

    Loader holds verified working-linear RGB/masks through staging. It admits
    download/conversion/model costs in addition to the native-stage costs below.
    Proposed always requires review; unsupported never becomes accepted.
    """
    if not all(callable(value) for value in (load_arrays,verify_runtime,verify_scene)):
        raise OperationFailure('CONFIG_INVALID')
    active=ctx.check_active;profile=deepcopy(ctx.lease.execution_profile)
    active();qualification=verify_runtime();active()
    params=SemanticTransferParams.model_validate_json(canonical_bytes(artifacts.params(SemanticTransferParams)))
    def document(ref,model):
        active();return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
    intent=document(params.intent,EditIntent);calibration=document(params.calibration,Calibration)
    params,intent,calibration=bind_semantic_transfer_documents(params,intent,calibration)
    if qualification is not None:
        from photo_worker.v2.semantic_qualification_pin import SemanticQualificationPin
        from photo_worker.v2.adoption import ArtifactRef
        if not isinstance(qualification,SemanticQualificationPin):raise OperationFailure('CONFIG_INVALID')
        qualification.bind(params.model_lock_sha,ArtifactRef(params.calibration.artifact_id,params.calibration.sha256))
        if calibration.dataset_sha!=qualification.dataset_sha:
            raise ValueError('SEMANTIC_CALIBRATION_DATASET_CHANGED')

    reference=document(params.reference.source,WorkingImage);target=document(params.target.source,WorkingImage)
    from photo_worker.contracts.base import canonical_hash
    if canonical_hash(reference.canvas)!=params.reference.canvas_hash or canonical_hash(target.canvas)!=params.target.canvas_hash:
        raise ValueError('SEMANTIC_SOURCE_CHANGED')
    costs=semantic_transfer_resources((reference.canvas.height,reference.canvas.width),(target.canvas.height,target.canvas.width))
    estimate=profile.get('resource_estimate') if isinstance(profile,dict) else None
    if not isinstance(estimate,dict) or any(type(estimate.get(k)) is not int or not 0<=estimate[k]<=2**53-1 for k in costs):
        raise OperationFailure('CONFIG_INVALID')
    for key,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),
            ('vram_bytes','VRAM_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED')]:
        if estimate[key]<costs[key]:raise OperationFailure(code)
    def recheck_qualification():
        if qualification is None:return
        active()
        current=verify_runtime()
        active()
        if current!=qualification:
            raise ValueError('SEMANTIC_RUNTIME_QUALIFICATION_CHANGED')

    ctx.progress(0,3)
    with load_arrays(artifacts,deepcopy(params),deepcopy(reference),deepcopy(target),deepcopy(profile),active) as arrays:
        if (not isinstance(arrays,SemanticTransferArrays)
                or arrays.reference.shape!=(reference.canvas.height,reference.canvas.width,3)
                or arrays.target.shape!=(target.canvas.height,target.canvas.width,3)):
            raise ValueError('SEMANTIC_SOURCE_CHANGED')
        active();ctx.progress(1,3)
        with prepare_calibrated_planar_transfer(params,intent,calibration,arrays.reference,arrays.target,arrays.mask,
                source_protected=arrays.reference_protected,target_protected=arrays.target_protected,
                scratch=artifacts.scratch,execution_profile=profile,verify_scene=verify_scene,check_active=active) as prepared:
            ctx.progress(2,3)
            recheck_qualification()
            staged=stage_planar_transfer(artifacts,prepared,params,reference,target,check_active=active)
            recheck_qualification()
            active();ctx.progress(3,3)
            return OperationResult(outputs=[staged.candidate_ref],review_required=staged.candidate.state=='proposed',
                bytes_written=staged.bytes_written)

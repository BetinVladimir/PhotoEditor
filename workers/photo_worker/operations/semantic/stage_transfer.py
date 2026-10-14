"""Stage complete V27 proposal closure inside owned mask scope."""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import SemanticTransferParams,TransferCandidate
from photo_worker.algorithms.semantic_transfer import validate_planar_homography
from .prepare_transfer import PreparedPlanarTransfer


@dataclass(frozen=True)
class StagedTransfer:
    candidate: TransferCandidate
    candidate_ref: object
    outputs: tuple
    bytes_written: int


def stage_planar_transfer(artifacts,prepared,params,reference_image,target_image,*,check_active=lambda:None):
    """Attempt staging only; source authority, review and adoption are external."""
    check_active()
    params=SemanticTransferParams.model_validate_json(canonical_bytes(params))
    reference_image=WorkingImage.model_validate_json(canonical_bytes(reference_image))
    target_image=WorkingImage.model_validate_json(canonical_bytes(target_image))
    if (not isinstance(prepared,PreparedPlanarTransfer)
            or canonical_hash(reference_image.canvas)!=params.reference.canvas_hash
            or canonical_hash(target_image.canvas)!=params.target.canvas_hash):
        raise ValueError('SEMANTIC_STAGE_BINDING_INVALID')
    match=prepared.match;outputs=[];mask_ref=transform_ref=None;written=0
    if match.reference_to_target is None:
        if prepared.mask is not None or not isinstance(match.reason,str) or not 1<=len(match.reason)<=80:
            raise ValueError('SEMANTIC_STAGE_BINDING_INVALID')
        state='unsupported';reason=match.reason
    else:
        matrix=np.array(match.reference_to_target,dtype=np.float64,copy=True)
        if (match.reason is not None or prepared.mask is None or prepared.mask.source_pixels<1
                or prepared.mask.target_pixels<1 or prepared.mask.pixels.flags.writeable
                or prepared.mask.pixels.shape!=(target_image.canvas.height,target_image.canvas.width)
                or not validate_planar_homography(matrix,(reference_image.canvas.height,reference_image.canvas.width))):
            raise ValueError('SEMANTIC_STAGE_BINDING_INVALID')
        transform=ResolvedTransform(schema_version=1,from_canvas_hash=params.reference.canvas_hash,
            to_canvas_hash=params.target.canvas_hash,matrix=matrix.ravel().tolist(),kind='homography')
        # Validate all typed metadata before the first external stage action.
        candidate=TransferCandidate(schema_version=1,target=params.target,mask=None,transform=None,
            confidence=match.inlier_fraction,state='proposed',reason=None)
        check_active()
        prepared.mask.reservation.validate_file(prepared.mask.path)
        written+=prepared.mask.path.stat().st_size
        if written>800004096:raise ValueError('SEMANTIC_STAGED_OUTPUT_LIMIT')
        pixels=artifacts.stage(prepared.mask.path,prepared.mask.reservation,'mask_pixels','application/x-npy')
        outputs.append(pixels)
        check_active()
        mask=MaskRef(artifact=pixels,canvas=target_image.canvas,transform_chain=target_image.transform_chain,
            selector='manual',confidence=candidate.confidence,model_lock_id=params.model_lock_sha,protected=False)
        written+=len(canonical_bytes(mask))
        mask_ref=artifacts.stage_contract(mask,'mask');outputs.append(mask_ref)
        check_active()
        written+=len(canonical_bytes(transform))
        transform_ref=artifacts.stage_contract(transform,'transform');outputs.append(transform_ref)
        state='proposed';reason=None
    candidate=TransferCandidate(schema_version=1,target=params.target,mask=mask_ref,transform=transform_ref,
        confidence=match.inlier_fraction,state=state,reason=reason)
    check_active()
    written+=len(canonical_bytes(candidate))
    if written>825169920:raise ValueError('SEMANTIC_STAGED_OUTPUT_LIMIT')
    result=artifacts.stage_contract(candidate,'document');outputs.append(result)
    check_active()
    return StagedTransfer(candidate,result,tuple(outputs),written)

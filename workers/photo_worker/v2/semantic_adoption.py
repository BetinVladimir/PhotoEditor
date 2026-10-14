"""Semantic proposal document geometry gate; binary/scene proof is separate."""
from hashlib import sha256
import numpy as np
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import WorkingImage,MaskRef,ResolvedTransform
from photo_worker.contracts.semantic import SemanticTransferParams,TransferCandidate
from photo_worker.algorithms.semantic_transfer import validate_planar_homography
from .adoption import ArtifactRef,AdoptionInvalid,MAX_JSON_BYTES


def verify_semantic_geometry(reader,params_ref,root_ref,*,check_active=lambda:None):
    """Require authenticated params/source and same-attempt proposal documents.

    This proves document geometry, not actual mask values, protected overlap,
    scene planarity, model qualification, source runtime or snapshot authority.
    Those must be verified independently before adopting the complete closure.
    """
    def ref(value):return ArtifactRef(value.artifact_id,value.sha256)
    def document(value,model,source):
        check_active();evidence=reader.evidence(value)
        if evidence.ref!=value or evidence.source!=source or evidence.media_type!='application/json':
            raise AdoptionInvalid('SEMANTIC_ADOPTION_EVIDENCE_INVALID')
        raw=reader.read_json(value,MAX_JSON_BYTES);check_active()
        if (not isinstance(raw,bytes) or len(raw)>MAX_JSON_BYTES or len(raw)!=evidence.bytes
                or sha256(raw).hexdigest()!=value.sha256):raise AdoptionInvalid('SEMANTIC_ADOPTION_BYTES_INVALID')
        try:return model.model_validate_json(raw)
        except ValueError as error:raise AdoptionInvalid('SEMANTIC_ADOPTION_CONTRACT_INVALID') from error
    params=document(params_ref,SemanticTransferParams,'input')
    candidate=document(root_ref,TransferCandidate,'output')
    if candidate.target!=params.target:raise AdoptionInvalid('SEMANTIC_ADOPTION_TARGET_CHANGED')
    if candidate.state!='proposed':
        if candidate.mask is not None or candidate.transform is not None or candidate.reason is None:
            raise AdoptionInvalid('SEMANTIC_ADOPTION_STATE_INVALID')
        return
    if candidate.mask is None or candidate.transform is None or candidate.reason is not None:
        raise AdoptionInvalid('SEMANTIC_ADOPTION_STATE_INVALID')
    reference=document(ref(params.reference.source),WorkingImage,'input')
    target=document(ref(params.target.source),WorkingImage,'input')
    mask=document(ref(candidate.mask),MaskRef,'output')
    transform=document(ref(candidate.transform),ResolvedTransform,'output')
    if canonical_hash(reference.canvas)!=params.reference.canvas_hash or canonical_hash(target.canvas)!=params.target.canvas_hash:
        raise AdoptionInvalid('SEMANTIC_ADOPTION_SOURCE_CHANGED')
    if (mask.canvas!=target.canvas or mask.transform_chain!=target.transform_chain or mask.protected
            or mask.selector!='manual' or mask.model_lock_id!=params.model_lock_sha
            or mask.confidence!=candidate.confidence):raise AdoptionInvalid('SEMANTIC_ADOPTION_MASK_CHANGED')
    if (transform.from_canvas_hash!=params.reference.canvas_hash or transform.to_canvas_hash!=params.target.canvas_hash
            or transform.kind!='homography' or not validate_planar_homography(
                np.asarray(transform.matrix).reshape(3,3),(reference.canvas.height,reference.canvas.width))):
        raise AdoptionInvalid('SEMANTIC_ADOPTION_TRANSFORM_INVALID')
    check_active();return mask


def verify_semantic_mask_pixels(reader,mask,*,scratch_root,max_bytes,check_active=lambda:None):
    """Verify proposed mask NPY bytes/layout/range and nonempty coverage.

    Caller must first bind this MaskRef to authenticated candidate/target
    documents. Protection and source-to-target warp equivalence are separate.
    """
    from photo_worker.contracts.base import canonical_bytes
    from .array_evidence import ArrayEvidenceSession
    mask=MaskRef.model_validate_json(canonical_bytes(mask));check_active()
    if mask.protected:raise AdoptionInvalid('SEMANTIC_ADOPTION_MASK_CHANGED')
    with ArrayEvidenceSession(reader,scratch_root,max_bytes,check_active) as arrays:
        try:
            pixels=arrays.array(ArtifactRef(mask.artifact.artifact_id,mask.artifact.sha256),
                (mask.canvas.height,mask.canvas.width),source='output',role='mask_pixels',unit=True)
        except ValueError as error:
            if not isinstance(error,AdoptionInvalid) and str(error).startswith('ARRAY_'):
                raise AdoptionInvalid('SEMANTIC_ADOPTION_MASK_ARRAY_INVALID') from error
            raise
        for step in mask.transform_chain.steps:
            if step.kind == 'dense_warp':
                arrays.array(ArtifactRef(step.inverse_field.artifact_id, step.inverse_field.sha256),
                    (step.to_canvas.height, step.to_canvas.width, 2), source='input',
                    legacy_input_npy=True, inverse_field=True)
        count=0
        for y in range(0,pixels.shape[0],256):
            for x in range(0,pixels.shape[1],256):
                check_active();count+=int(np.count_nonzero(pixels[y:y+256,x:x+256]>0))
        if count==0:raise AdoptionInvalid('SEMANTIC_ADOPTION_MASK_EMPTY')
        check_active();return count

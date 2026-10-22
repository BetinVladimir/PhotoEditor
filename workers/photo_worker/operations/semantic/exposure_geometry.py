"""Exact proposal geometry document binding; no array loading or re-warping."""
from hashlib import sha256
import numpy as np
from photo_worker.algorithms.semantic_transfer import validate_planar_homography
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import MaskRef,ResolvedTransform
from .approved_binding import bind_approved_edit_documents
from .exposure_source import bind_exposure_working_source


def bind_local_exposure_geometry(edit,candidate_raw,intent_raw,source_raw,mask_raw,transform_raw,reference_raw):
    edit,candidate,intent=bind_approved_edit_documents(edit,candidate_raw,intent_raw)
    return bind_exposure_geometry_documents(edit.source,candidate,intent,source_raw,mask_raw,transform_raw,reference_raw)


def bind_exposure_geometry_documents(source,candidate,intent,source_raw,mask_raw,transform_raw,reference_raw):
    """Shared exact geometry proof; caller first binds candidate/intent documents."""
    image=bind_exposure_working_source(source,source_raw)
    reference=bind_exposure_working_source(intent.reference,reference_raw)
    def document(raw,ref,model):
        if not isinstance(raw,(bytes,bytearray)) or not 2<=len(raw)<=1048576:
            raise ValueError('SEMANTIC_EXPOSURE_GEOMETRY_INVALID')
        raw=bytes(raw)
        if sha256(raw).hexdigest()!=ref.sha256:
            raise ValueError('SEMANTIC_EXPOSURE_GEOMETRY_HASH')
        return model.model_validate_json(canonical_bytes(parse_json(raw)))
    mask=document(mask_raw,candidate.mask,MaskRef)
    transform=document(transform_raw,candidate.transform,ResolvedTransform)
    if (mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain
            or mask.artifact.artifact_id.int==0 or mask.protected or mask.selector!='manual' or mask.model_lock_id is None
            or mask.confidence!=candidate.confidence
            or transform.kind!='homography' or transform.schema_version!=1
            or transform.to_canvas_hash!=source.canvas_hash
            or transform.from_canvas_hash!=intent.reference.canvas_hash):
        raise ValueError('SEMANTIC_EXPOSURE_GEOMETRY_CHANGED')
    if not validate_planar_homography(np.asarray(transform.matrix,dtype=np.float64).reshape(3,3),
            (reference.canvas.height,reference.canvas.width)):
        raise ValueError('SEMANTIC_EXPOSURE_TRANSFORM_UNSAFE')
    return image,mask,transform

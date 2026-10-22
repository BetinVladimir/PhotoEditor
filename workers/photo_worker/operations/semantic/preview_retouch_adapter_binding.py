"""Exact full-face retouch content binding; live qualification remains external."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from .preview_binding import bind_retouch_preview_documents


def bind_retouch_preview_adapter(params,candidate_raw,intent_raw,context_ref,context_raw,mask_raw):
    owned,candidate,intent=bind_retouch_preview_documents(params,candidate_raw,intent_raw)
    def document(ref,raw,model):
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=ref.sha256:
            raise ValueError('SEMANTIC_RETOUCH_CONTEXT_BYTES_INVALID')
        value=model.model_validate_json(canonical_bytes(parse_json(raw)))
        if canonical_bytes(value)!=raw:raise ValueError('SEMANTIC_RETOUCH_CONTEXT_BYTES_INVALID')
        return value
    context=document(context_ref,context_raw,RetouchPreviewAdapterContext)
    mask=document(candidate.mask,mask_raw,MaskRef)
    if (context.source!=owned.source or context.candidate!=owned.candidate
            or context.transferred_mask!=candidate.mask or context.protection!=owned.protection
            or (context.model_lock_sha,context.runtime_lock_sha,context.device_profile_sha)!=(
                owned.model_lock_sha,owned.runtime_lock_sha,owned.device_profile_sha)
            or mask.protected or mask.selector!='manual'
            or any(face.manual!=mask for face in context.faces)):
        raise ValueError('SEMANTIC_RETOUCH_CONTEXT_BINDING_INVALID')
    roots=(owned.plan,owned.intent,owned.candidate,owned.source.source,owned.protection,
        candidate.mask,candidate.transform,context_ref)
    ids={ref.artifact_id for ref in roots}
    if (context_ref.artifact_id.int==0 or len(ids)!=len(roots)
            or mask.artifact.artifact_id.int==0 or mask.artifact.artifact_id in ids
            or any(face.landmarks.artifact_id in ids or face.landmarks.artifact_id==mask.artifact.artifact_id
                for face in context.faces)):
        raise ValueError('SEMANTIC_RETOUCH_CONTEXT_ALIAS')
    landmark_models={}
    artifact_contracts={}
    def declare(ref,contract):
        value=(contract,ref.sha256)
        if artifact_contracts.setdefault(ref.artifact_id,value)!=value:
            raise ValueError('SEMANTIC_RETOUCH_CONTEXT_ALIAS')
    declare(mask.artifact,'NpyArray')
    for face in context.faces:
        if (face.object_id is not None and face.object_id.int==0
                or face.skin is not None and (face.skin.artifact.artifact_id.int==0
                    or face.skin.artifact.artifact_id in ids)):
            raise ValueError('SEMANTIC_RETOUCH_CONTEXT_ALIAS')
        # A declared artifact has one contract: it cannot simultaneously be a
        # standalone Face68 and a FaceLandmarks collection for another entry.
        collection=face.object_id is not None
        declare(face.landmarks,'FaceLandmarks' if collection else 'Face68')
        if face.skin is not None:declare(face.skin.artifact,'NpyArray')
        previous=landmark_models.setdefault(face.landmarks.artifact_id,collection)
        if previous!=collection:
            raise ValueError('SEMANTIC_RETOUCH_FACE_CONTEXT_INVALID')
    return owned,candidate,intent,context

"""Own full-face geometry/provenance binding before numerical retouch."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.contracts.images import Face68
from photo_worker.contracts.face import FaceLandmarks
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from .exposure_source import bind_exposure_working_source


def bind_retouch_preview_faces(context,source_raw,*,read_landmarks,verify_origin,check_active):
    if not all(callable(fn) for fn in (read_landmarks,verify_origin,check_active)):
        raise ValueError('SEMANTIC_RETOUCH_FACE_INSTALLATION_REQUIRED')
    owned=RetouchPreviewAdapterContext.model_validate_json(canonical_bytes(context))
    check_active()
    image=bind_exposure_working_source(owned.source,source_raw)
    result=[];seen=set()
    for entry in owned.faces:
        check_active()
        if verify_origin(entry.landmarks.model_copy(deep=True),owned.source.model_copy(deep=True)) is not True:
            check_active();raise ValueError('SEMANTIC_RETOUCH_FACE_ORIGIN_STALE')
        check_active()
        raw=read_landmarks(entry.landmarks.model_copy(deep=True))
        check_active()
        if not isinstance(raw,bytes) or not 2<=len(raw)<=1048576 or sha256(raw).hexdigest()!=entry.landmarks.sha256:
            raise ValueError('SEMANTIC_RETOUCH_FACE_BYTES_INVALID')
        value=canonical_bytes(parse_json(raw))
        if value!=raw:
            raise ValueError('SEMANTIC_RETOUCH_FACE_BYTES_INVALID')
        if entry.object_id is None:
            face=Face68.model_validate_json(value)
        else:
            collection=FaceLandmarks.model_validate_json(value)
            matches=[face for face in collection.landmarks if face.object_id==entry.object_id]
            if collection.image!=owned.source.source or len(matches)!=1:
                raise ValueError('SEMANTIC_RETOUCH_FACE_SOURCE_INVALID')
            face=matches[0]
        if (face.object_id.int==0 or face.object_id in seen or face.canvas!=image.canvas
                or face.transform_chain!=image.transform_chain
                or entry.manual.canvas!=image.canvas or entry.manual.transform_chain!=image.transform_chain
                or entry.manual.object_id is not None and entry.manual.object_id!=face.object_id):
            raise ValueError('SEMANTIC_RETOUCH_FACE_GEOMETRY_INVALID')
        check_active()
        accepted=verify_origin(entry.landmarks.model_copy(deep=True),owned.source.model_copy(deep=True))
        check_active()
        if accepted is not True:raise ValueError('SEMANTIC_RETOUCH_FACE_ORIGIN_STALE')
        seen.add(face.object_id);result.append(face)
    check_active()
    return image,tuple(result)

"""Load only frozen, declared retouch context under a live task lease."""
from copy import deepcopy
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic import TransferCandidate,EditIntent
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from photo_worker.contracts.images import MaskRef
from .preview_binding import bind_retouch_preview_documents
from .preview_retouch_adapter_binding import bind_retouch_preview_adapter
from .preview_retouch_inputs import retouch_declared_inputs


def load_retouch_preview_context(ctx,artifacts,params,context_ref,*,verify_admission,verify_qualification):
    if not all(callable(fn) for fn in (verify_admission,verify_qualification)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    lease=deepcopy(ctx.lease);command=lease.command
    owned=SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    context_ref=ArtifactRef.model_validate_json(canonical_bytes(context_ref))
    def active():
        ctx.check_active()
        if ctx.lease!=lease:raise ValueError('SEMANTIC_RETOUCH_LEASE_CHANGED')
    active()
    if (command.operation!='semantic.preview_retouch_region' or command.operation_version!='1'
            or command.tenant_id!=owned.tenant_id
            or command.params.sha256!=sha256(canonical_bytes(owned)).hexdigest()):
        raise ValueError('SEMANTIC_RETOUCH_TASK_INVALID')
    inputs=retouch_declared_inputs(artifacts,check_active=active)
    declared={ref.artifact_id:ref.sha256 for ref in inputs}
    def required(ref):
        if declared.get(ref.artifact_id)!=ref.sha256:
            raise ValueError('SEMANTIC_RETOUCH_UNDECLARED_INPUT')
    roots=(owned.plan,owned.intent,owned.candidate,owned.source.source,owned.protection)
    if context_ref.artifact_id.int==0 or context_ref.artifact_id in {ref.artifact_id for ref in roots}:
        raise ValueError('SEMANTIC_RETOUCH_CONTEXT_ALIAS')
    for ref in (*roots,context_ref):required(ref)
    def admitted():
        active()
        if verify_admission(deepcopy(lease),owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        active()
    def read(ref,model):
        required(ref);admitted()
        raw=artifacts.contract_bytes(ref.model_copy(deep=True),model)
        admitted()
        return raw
    candidate_raw=read(owned.candidate,TransferCandidate)
    intent_raw=read(owned.intent,EditIntent)
    _,candidate,_=bind_retouch_preview_documents(owned,candidate_raw,intent_raw)
    required(candidate.mask);required(candidate.transform)
    context_raw=read(context_ref,RetouchPreviewAdapterContext)
    mask_raw=read(candidate.mask,MaskRef)
    result=bind_retouch_preview_adapter(owned,candidate_raw,intent_raw,context_ref,context_raw,mask_raw)
    context=result[3]
    for face in context.faces:
        required(face.landmarks);required(face.manual.artifact)
        if face.skin is not None:required(face.skin.artifact)
    admitted()
    qualified=verify_qualification(deepcopy(lease),owned.model_copy(deep=True),context_ref.model_copy(deep=True),bytes(context_raw))
    active()
    if qualified is not True:raise ValueError('SEMANTIC_RETOUCH_QUALIFICATION_DENIED')
    admitted()
    return result


def load_retouch_preview_face_context(ctx,artifacts,params,context_ref,*,verify_admission,
        verify_qualification,verify_face_origin):
    """Bind all face documents before numerical arrays/ROI processing."""
    from photo_worker.contracts.images import WorkingImage,Face68
    from photo_worker.contracts.face import FaceLandmarks
    from .preview_retouch_face_geometry import bind_retouch_preview_faces
    if not callable(verify_face_origin):raise ValueError('SEMANTIC_RETOUCH_FACE_INSTALLATION_REQUIRED')
    lease=deepcopy(ctx.lease)
    result=load_retouch_preview_context(ctx,artifacts,params,context_ref,
        verify_admission=verify_admission,verify_qualification=verify_qualification)
    owned,_,_,context=result
    pinned=ArtifactRef.model_validate_json(canonical_bytes(context_ref))
    def active():
        ctx.check_active()
        if ctx.lease!=lease:raise ValueError('SEMANTIC_RETOUCH_LEASE_CHANGED')
    def admitted():
        active()
        accepted=verify_admission(deepcopy(lease),owned.model_copy(deep=True))
        active()
        if accepted is not True:raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    declared={ref.artifact_id:ref.sha256 for ref in retouch_declared_inputs(artifacts,check_active=admitted)}
    def read(ref,model):
        if declared.get(ref.artifact_id)!=ref.sha256:raise ValueError('SEMANTIC_RETOUCH_UNDECLARED_INPUT')
        admitted();raw=artifacts.contract_bytes(ref.model_copy(deep=True),model);admitted();return raw
    models={}
    for entry in context.faces:
        model=Face68 if entry.object_id is None else FaceLandmarks
        if entry.landmarks.artifact_id in models and models[entry.landmarks.artifact_id] is not model:
            raise ValueError('SEMANTIC_RETOUCH_FACE_CONTEXT_INVALID')
        models[entry.landmarks.artifact_id]=model
    source_raw=read(owned.source.source,WorkingImage)
    def origin(ref,source):
        admitted()
        accepted=verify_face_origin(deepcopy(lease),ref,source)
        admitted();return accepted is True
    image,faces=bind_retouch_preview_faces(context,source_raw,
        read_landmarks=lambda ref:read(ref,models[ref.artifact_id]),verify_origin=origin,check_active=admitted)
    admitted()
    qualified=verify_qualification(deepcopy(lease),owned.model_copy(deep=True),pinned,canonical_bytes(context))
    active()
    if qualified is not True:raise ValueError('SEMANTIC_RETOUCH_QUALIFICATION_DENIED')
    admitted()
    return (*result,image,faces)

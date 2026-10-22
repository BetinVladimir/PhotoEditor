"""Existing full-face beauty with exact region/permanent preservation proof."""
from contextlib import contextmanager
from dataclasses import dataclass,asdict
import numpy as np
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.images import WorkingImage,Face68
from photo_worker.contracts.beauty import BeautyFaceReport
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.contracts.semantic_retouch import RetouchPreviewAdapterContext
from photo_worker.algorithms.effective import effective_beauty_params
from photo_worker.operations.portrait.prepare import prepare_beauty,FaceInput,FaceEvidence
from photo_worker.runtime.arrays import read_array
from .candidate_preservation import verify_candidate_preservation


@dataclass(frozen=True)
class RetouchPreviewComputed:
    pixels: object
    path: object
    preservation: object
    faces: tuple
    peak_bytes: int


@contextmanager
def compute_retouch_preview(params,context,image,rgb,mask,protected,faces,scratch,*,strength,
        max_ram_bytes,verify_admission,check_active,alpha=None):
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned=SemanticRetouchPreviewInput.model_validate_json(canonical_bytes(params))
    context=RetouchPreviewAdapterContext.model_validate_json(canonical_bytes(context))
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    if not isinstance(faces,tuple) or not 1<=len(faces)<=64:
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    faces=tuple(Face68.model_validate_json(canonical_bytes(face)) for face in faces)
    h,w=image.canvas.height,image.canvas.width
    if (context.source!=owned.source or context.candidate!=owned.candidate or context.protection!=owned.protection
            or canonical_hash(image)!=owned.source.source.sha256
            or canonical_hash(image.canvas)!=owned.source.canvas_hash or len(faces)!=len(context.faces)
            or any(face.object_id.int==0 or face.canvas!=image.canvas or face.transform_chain!=image.transform_chain for face in faces)
            or any(binding.manual.canvas!=image.canvas or binding.manual.transform_chain!=image.transform_chain
                or binding.manual.object_id is not None and binding.manual.object_id!=face.object_id
                for binding,face in zip(context.faces,faces))
            or any(binding.object_id is not None and binding.object_id!=face.object_id
                for binding,face in zip(context.faces,faces))):
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    if not isinstance(rgb,np.ndarray) or rgb.shape!=(h,w,3) or len({face.object_id for face in faces})!=len(faces):
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    if (context.model_lock_sha,context.runtime_lock_sha,context.device_profile_sha)!=(
            owned.model_lock_sha,owned.runtime_lock_sha,owned.device_profile_sha):
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    if (not 1<=h*w<=200000000 or (image.alpha is None)!=(alpha is None)):
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    if (rgb.dtype!=np.float32
            or any(not isinstance(value,np.ndarray) or value.shape!=(h,w)
                or value.dtype not in (np.dtype(np.float32),np.dtype(np.bool_)) for value in (mask,protected))
            or alpha is not None and (not isinstance(alpha,np.ndarray)
                or alpha.dtype!=np.float32 or alpha.shape!=(h,w))):
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    effective=effective_beauty_params(context.beauty,strength)
    peak=512*1048576+128*h*w+256*4194304
    if type(max_ram_bytes) is not int or not peak<=max_ram_bytes<=2**53-1:
        raise ValueError('SEMANTIC_RETOUCH_RESOURCE_PROFILE')
    def admitted():
        check_active()
        accepted=verify_admission(owned.model_copy(deep=True),context.model_copy(deep=True))
        check_active()
        if accepted is not True:raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
    # Snapshot before invoking authority callbacks: callbacks must not change
    # the numeric values being validated by mutating caller-owned buffers.
    check_active()
    source=rgb.copy()
    check_active()
    mask=mask.astype(np.float32,copy=True)
    check_active()
    protected=protected.astype(np.float32,copy=True)
    check_active()
    owned_alpha=None if alpha is None else alpha.copy()
    check_active()
    admitted()
    verify_candidate_preservation(source,source,mask,protected,
        original_alpha=owned_alpha,candidate_alpha=owned_alpha,
        max_workspace_bytes=8*1048576,check_active=admitted)
    for y in range(0,h,256):
        admitted()
        if np.any(source[y:y+256]>1):raise ValueError('SEMANTIC_RETOUCH_HDR_UNSUPPORTED')
    if source.shape!=(h,w,3) or (image.alpha is None)!=(owned_alpha is None):
        raise ValueError('SEMANTIC_RETOUCH_COMPUTE_BINDING_INVALID')
    permanent=np.empty((h,w),dtype=np.float32)
    for y in range(0,h,256):
        admitted();permanent[y:y+256]=((protected[y:y+256]>0)|(mask[y:y+256]==0)).astype(np.float32)
    for value in (source,mask,protected,permanent,owned_alpha):
        if value is not None:value.flags.writeable=False
    inputs=tuple(FaceInput(face,None,mask,True) for face in faces)
    admitted()
    with prepare_beauty(source,image,inputs,effective,scratch,permanent=permanent,
            max_roi_pixels=4194304,check_active=admitted) as files:
        admitted()
        if (not isinstance(files.faces,tuple) or len(files.faces)!=len(faces)
                or any(type(report) is not FaceEvidence for report in files.faces)
                or {report.object_id for report in files.faces}!={face.object_id for face in faces}):
            raise ValueError('SEMANTIC_RETOUCH_FACE_EVIDENCE_INVALID')
        for report in files.faces:
            BeautyFaceReport.model_validate(asdict(report))
            if (report.applied and report.reason is not None
                    or not report.applied and report.reason is None):
                raise ValueError('SEMANTIC_RETOUCH_FACE_EVIDENCE_INVALID')
        with read_array(files.image,(h,w,3),'float32',admitted) as output:
            proof=verify_candidate_preservation(source,output,mask,protected,
                original_alpha=owned_alpha,candidate_alpha=owned_alpha,
                max_workspace_bytes=8*1048576,check_active=admitted)
            if effective.smoothing==0 and effective.tone_evenness==0 and proof.changed_pixels!=0:
                raise ValueError('SEMANTIC_RETOUCH_ZERO_STRENGTH_CHANGED')
            admitted()
            yield RetouchPreviewComputed(output,files.image,proof,files.faces,peak)
            admitted()

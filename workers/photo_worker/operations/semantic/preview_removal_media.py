"""Removal display media with independent exact preservation verification."""
from dataclasses import dataclass
import numpy as np
from .preview_binding import bind_removal_preview_documents,bind_removal_preview_source
from .preview_removal_loader import RemovalPreviewArrays
from .candidate_preservation import verify_candidate_preservation
from .preview_presentation import prepare_exposure_display,ExposureDisplay
from .preview_overlay import prepare_preview_mask_overlay


@dataclass(frozen=True)
class PreparedPreviewMedia:
    before: ExposureDisplay
    after: ExposureDisplay
    affected: np.ndarray
    protected: np.ndarray
    width: int
    height: int


def prepare_removal_preview_media(params,candidate_raw,intent_raw,source_raw,arrays,encoded,alpha,
        *,icc_root,icc_lock_sha,srgb_sha,max_bytes,verify_admission,verify_runtime,verify_color,check_active):
    """Array producer/geometry provenance is mandatory at the caller's loader.

    Own all input arrays before runtime callbacks. Repeat independent preservation
    verification before display conversion; never use display clipping as edit output.
    """
    if not all(callable(fn) for fn in (verify_admission,verify_runtime,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned,candidate,intent=bind_removal_preview_documents(params,candidate_raw,intent_raw)
    image=bind_removal_preview_source(owned,source_raw)
    shape=(image.canvas.height,image.canvas.width);pixels=shape[0]*shape[1]
    if (type(max_bytes) is not int or not 640*pixels+128*1024**2<=max_bytes<=2**53-1
            or not isinstance(arrays,RemovalPreviewArrays)
            or any(not isinstance(value,np.ndarray) or value.dtype!=np.float32
                or value.shape!=(*shape,3) for value in (arrays.original,encoded))
            or any(not isinstance(value,np.ndarray) or value.dtype!=np.float32
                or value.shape!=shape for value in (arrays.mask,arrays.protected))
            or (image.alpha is None)!=(alpha is None)
            or (alpha is not None and (not isinstance(alpha,np.ndarray)
                or alpha.dtype!=np.float32 or alpha.shape!=shape))):
        raise ValueError('SEMANTIC_PREVIEW_MEDIA_INVALID')
    check_active()
    original=arrays.original.copy()
    check_active();changed=encoded.copy()
    check_active();mask=arrays.mask.copy()
    check_active();protection=arrays.protected.copy()
    check_active();opacity=alpha.copy() if alpha is not None else None
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True or verify_runtime() is not True:
            raise ValueError('SEMANTIC_PREVIEW_MEDIA_ADMISSION_DENIED')
        check_active()
    admitted()
    available=max_bytes-128*pixels-64*1024**2
    if verify_color(image.color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_PREVIEW_MEDIA_ICC_UNQUALIFIED')
    admitted()
    verify_candidate_preservation(original,original,mask,protection,
        original_alpha=opacity,candidate_alpha=opacity,
        max_workspace_bytes=8*1048576,check_active=check_active)
    # Proof and overlay use the same discrete editing region as compute.
    selected=mask>=.5
    guarded=protection>0
    verify_candidate_preservation(original,changed,selected,guarded,
        original_alpha=opacity,candidate_alpha=opacity,
        max_workspace_bytes=8*1048576,check_active=check_active)
    mask=(selected & ~guarded).astype(np.float32)
    protection=guarded.astype(np.float32)
    # Same no-upscale 2560 edge convention as existing media preview2560.
    scale=min(1.,2560/max(shape));width=max(1,round(shape[1]*scale));height=max(1,round(shape[0]*scale))
    common=dict(width=width,height=height,icc_root=icc_root,icc_lock_sha=icc_lock_sha,
        srgb_sha=srgb_sha,verify_runtime=verify_runtime,check_active=check_active,max_bytes=available)
    admitted();before=prepare_exposure_display(image,original,opacity,**common)
    admitted();after=prepare_exposure_display(image,changed,opacity,**common)
    admitted();affected=prepare_preview_mask_overlay(mask,width=width,height=height,protected=False,
        max_bytes=available,check_active=check_active)
    admitted();protected=prepare_preview_mask_overlay(protection,width=width,height=height,protected=True,
        max_bytes=available,check_active=check_active)
    admitted()
    return PreparedPreviewMedia(before,after,affected,protected,width,height)

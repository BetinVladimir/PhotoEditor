"""Pre-Apply exposure computation only; no artifact publication or Apply receipt."""
from dataclasses import dataclass
import numpy as np
from photo_worker.operations.optical.color import to_working_linear
from .preview_binding import bind_exposure_preview_documents, bind_exposure_preview_source
from .local_exposure import apply_local_exposure
from .exposure_encoding import restore_local_exposure_encoding
from .exposure_quality import verify_exposure_pixels


@dataclass(frozen=True)
class PreviewExposureArrays:
    original: np.ndarray
    mask: np.ndarray
    protected: np.ndarray
    loader_peak_bytes: int
    retained_loader_bytes: int


def execute_exposure_preview(params,candidate_raw,intent_raw,source_raw,*,
        verify_admission,load_verified_arrays,verify_color,max_bytes,check_active):
    """Loader must prove exact source/mask/transform/protection and qualification.

    Preview admission is separate from human Apply admission. Both callbacks
    receive owned proposal inputs; neither has a permissive default. Loader
    must enforce its supplied budget before IO and include all retained owners.
    """
    if not all(callable(value) for value in
            (verify_admission,load_verified_arrays,verify_color,check_active)):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    owned,candidate,intent=bind_exposure_preview_documents(params,candidate_raw,intent_raw)
    image=bind_exposure_preview_source(owned,source_raw)
    shape=(image.canvas.height,image.canvas.width)
    pixels=shape[0]*shape[1]
    # Conservative complete compute peak: loader arrays, owned inputs,
    # linear conversion, kernel, encoding and independent quality replay.
    required=512*pixels+64*1024**2
    if type(max_bytes) is not int or not required<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def admitted():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    admitted()
    arrays=load_verified_arrays(owned.model_copy(deep=True),candidate.model_copy(deep=True),
        image.model_copy(deep=True),max_bytes=max_bytes)
    if (not isinstance(arrays,PreviewExposureArrays)
            or not isinstance(arrays.original,np.ndarray) or arrays.original.dtype!=np.float32
            or arrays.original.shape!=(*shape,3)
            or any(not isinstance(value,np.ndarray) or value.dtype!=np.float32
                or value.shape!=shape for value in (arrays.mask,arrays.protected))):
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    if (type(arrays.loader_peak_bytes) is not int or type(arrays.retained_loader_bytes) is not int
            or not 20*pixels<=arrays.loader_peak_bytes<=max_bytes
            or not 0<=arrays.retained_loader_bytes<=arrays.loader_peak_bytes
            or arrays.retained_loader_bytes+required>max_bytes):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    budget=max_bytes-arrays.retained_loader_bytes
    original=arrays.original.copy();mask=arrays.mask.copy();protected=arrays.protected.copy()
    admitted()
    if verify_color(image.color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    admitted()
    linear=to_working_linear(original,image.color)
    changed=apply_local_exposure(linear,mask,protected,ev=intent.ev,strength=intent.strength,
        max_bytes=budget,check_active=check_active)
    admitted()
    encoded=restore_local_exposure_encoding(original,changed,mask,protected,image.color,
        max_bytes=budget,check_active=check_active)
    verify_exposure_pixels(original,encoded,mask,protected,image.color,
        ev=intent.ev,strength=intent.strength,max_bytes=budget,
        verify_color=verify_color,check_active=check_active)
    admitted()
    return encoded

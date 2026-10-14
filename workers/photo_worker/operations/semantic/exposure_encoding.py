"""Restore source encoding without changing unedited/protected pixel bits."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.optical.color import to_working_linear,from_working_linear
from .local_exposure import local_exposure_memory


def restore_local_exposure_encoding(original,linear_result,mask,protected,color,*,max_bytes,check_active):
    color=ColorSpace.model_validate_json(canonical_bytes(color))
    if color.profile_id!='work_rec2020_gamma24_v1' or color.encoding!='gamma24':
        raise ValueError('SEMANTIC_EXPOSURE_COLOR_UNSUPPORTED')
    if not callable(check_active):raise ValueError('SEMANTIC_EXPOSURE_CANCELLATION_INVALID')
    if (not isinstance(original,np.ndarray) or original.dtype!=np.float32 or original.ndim!=3 or original.shape[2]!=3
            or not isinstance(linear_result,np.ndarray) or linear_result.dtype!=np.float32 or linear_result.shape!=original.shape
            or any(not isinstance(value,np.ndarray) or value.dtype!=np.float32 or value.shape!=original.shape[:2] for value in (mask,protected))):
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    required=local_exposure_memory(original.shape[:2])*2
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1 or required>max_bytes:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    # Own all aliases before cancellation callbacks can mutate caller inputs.
    original=original.copy();linear_result=linear_result.copy();mask=mask.copy();protected=protected.copy()
    check_active()
    output=original.copy()
    for y in range(0,original.shape[0],256):
        for x in range(0,original.shape[1],256):
            check_active()
            before=original[y:y+256,x:x+256];after=linear_result[y:y+256,x:x+256]
            weights=mask[y:y+256,x:x+256];protection=protected[y:y+256,x:x+256]
            if (not np.isfinite(before).all() or np.any(before<0) or not np.isfinite(after).all() or np.any(after<0)
                    or any(not np.isfinite(value).all() or np.any(value<0) or np.any(value>1) for value in (weights,protection))):
                raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
            if np.any((weights>0)&(protection>0)):
                raise ValueError('SEMANTIC_EXPOSURE_PROTECTED_OVERLAP')
            with np.errstate(under='ignore',divide='raise'):
                baseline=to_working_linear(before,color)
            changed=(weights>0)&np.any(after!=baseline,axis=2)
            if changed.any():
                with np.errstate(under='ignore',divide='raise'):
                    encoded=from_working_linear(after,color)
                np.copyto(output[y:y+256,x:x+256],encoded,where=changed[...,None])
    check_active()
    return output

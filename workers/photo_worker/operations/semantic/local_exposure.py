"""Masked exposure kernel for explicitly approved, verified linear RGB inputs.

No admission, ICC conversion, object loading or success receipt occurs here.
The installer must provide owned working-linear arrays after source/mask/color
and protected-region proof; the Apply commit remains a separate authority.
"""
import math
import numpy as np


def local_exposure_memory(shape):
    """Conservative logical-array RAM requirement; decoder/model costs external."""
    if (not isinstance(shape,(tuple,list)) or len(shape)!=2
            or any(type(value) is not int or value<1 for value in shape)
            or shape[0]>200_000_000//shape[1]):
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    return shape[0]*shape[1]*96


def apply_local_exposure(rgb_linear, mask, protected, *, ev, strength, max_bytes, check_active=None):
    """RGB * 2**(EV * strength * mask), preserving zero-mask pixels exactly.

    Budget conservatively covers resident input arrays, owned copies, output
    and numpy scratch; it is checked before any image-sized allocation.
    Protected overlap is rejected rather than silently clearing mask pixels.
    """
    if (type(ev) not in (int,float) or type(strength) not in (int,float)
            or not -1<=ev<=1 or not math.isfinite(ev)
            or not 0<=strength<=1 or not math.isfinite(strength)
            or type(max_bytes) is not int or not 1<=max_bytes<=2**53-1):
        raise ValueError('SEMANTIC_EXPOSURE_PARAMETERS_INVALID')
    if (not isinstance(rgb_linear,np.ndarray) or rgb_linear.dtype!=np.float32
            or rgb_linear.ndim!=3 or rgb_linear.shape[2]!=3
            or not isinstance(mask,np.ndarray) or mask.dtype!=np.float32
            or not isinstance(protected,np.ndarray) or protected.dtype!=np.float32
            or mask.shape!=rgb_linear.shape[:2] or protected.shape!=mask.shape):
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    required=local_exposure_memory(rgb_linear.shape[:2])
    if required>max_bytes:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    if check_active is not None and not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_CANCELLATION_INVALID')
    rgb=np.array(rgb_linear,dtype=np.float32,copy=True,order='C')
    weights=np.array(mask,dtype=np.float32,copy=True,order='C')
    protection=np.array(protected,dtype=np.float32,copy=True,order='C')
    if check_active is None:
        check_active=lambda:None
    # Own inputs before cancellation/lease callbacks; they cannot substitute
    # pixel or mask values midway through processing.
    check_active()
    height,width=rgb.shape[:2]
    for y in range(0,height,256):
        for x in range(0,width,256):
            check_active()
            tile=rgb[y:y+256,x:x+256]
            weight=weights[y:y+256,x:x+256]
            protected_tile=protection[y:y+256,x:x+256]
            if (not np.isfinite(tile).all() or (tile<0).any()
                    or not np.isfinite(weight).all() or (weight<0).any() or (weight>1).any()
                    or not np.isfinite(protected_tile).all() or (protected_tile<0).any() or (protected_tile>1).any()):
                raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
            if np.any((weight>0)&(protected_tile>0)):
                raise ValueError('SEMANTIC_EXPOSURE_PROTECTED_OVERLAP')
    output=rgb.copy()
    try:
        with np.errstate(over='raise',invalid='raise',under='ignore',divide='raise'):
            for y in range(0,height,256):
                for x in range(0,width,256):
                    check_active()
                    weight=weights[y:y+256,x:x+256]
                    factor=np.exp2(weight*np.float32(ev*strength))
                    np.multiply(rgb[y:y+256,x:x+256],factor[...,None],
                                out=output[y:y+256,x:x+256],where=(weight>0)[...,None])
    except FloatingPointError:
        raise ValueError('SEMANTIC_EXPOSURE_NONFINITE_OUTPUT') from None
    check_active()
    return output


def local_exposure_execution_memory(shape, *, loader_peak_bytes, retained_loader_bytes):
    """Whole-operation RAM admission, including verified loader costs.

    loader_peak_bytes includes all allocations while loading/converting inputs.
    retained_loader_bytes excludes RGB/mask/protected arrays already covered by
    kernel cost, and includes other loader owners retained during execution.
    Estimates must come from the installed loader; there is no inferred default.
    """
    kernel=local_exposure_memory(shape)
    if (type(loader_peak_bytes) is not int or type(retained_loader_bytes) is not int
            or not 0<=retained_loader_bytes<=loader_peak_bytes<=2**53-1
            or retained_loader_bytes>2**53-1-kernel):
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    return max(loader_peak_bytes,retained_loader_bytes+kernel)

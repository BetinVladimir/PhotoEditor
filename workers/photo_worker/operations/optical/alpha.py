"""Bounded legacy alpha normalization for optical linear resampling."""
from contextlib import contextmanager
import numpy as np
from photo_worker.algorithms.warp import validate_shape


@contextmanager
def normalized_alpha(alpha,shape,scratch,*,tile=512,check_active=lambda:None):
    """Yield float32 straight opacity; missing alpha means an opaque source.

    UInt16 conversion uses exactly division by65535. Float32 input is validated
    and borrowed read-only by convention; caller never mutates it. Disk-backed
    temporary output is closed and released on normal exit or cancellation.
    """
    validate_shape(shape)
    if type(tile) is not int or not 1<=tile<=2048:raise ValueError('LENS_PARAMS_INVALID')
    if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.shape!=shape or
                              alpha.dtype not in (np.dtype('uint16'),np.dtype('float32'))):
        raise ValueError('LENS_ALPHA_INVALID')
    height,width=shape
    if alpha is not None and alpha.dtype==np.float32:
        for top in range(0,height,tile):
            for left in range(0,width,tile):
                check_active();block=alpha[top:top+tile,left:left+tile]
                if not np.isfinite(block).all() or np.any((block<0)|(block>1)):
                    raise ValueError('LENS_ALPHA_INVALID')
        yield alpha
        return
    with scratch.reserve(height*width*4+4096) as reservation:
        path=reservation.path('lens-source-opacity','.npy')
        mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
        try:
            for top in range(0,height,tile):
                for left in range(0,width,tile):
                    check_active()
                    mapped[top:top+tile,left:left+tile]=(1. if alpha is None else
                        alpha[top:top+tile,left:left+tile].astype(np.float32)/np.float32(65535))
            mapped.flush();reservation.validate();check_active()
            yield mapped
        finally:mapped._mmap.close()

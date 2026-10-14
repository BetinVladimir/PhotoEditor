"""Bounded verification of materialized raster outputs before any staging."""
from collections.abc import Callable
import numpy as np
from photo_worker.algorithms.manual_mask import rasterize
from photo_worker.contracts.manual_mask import ManualMask


def verify_raster(mask:ManualMask,source_sha256:str,target:np.ndarray,protected:np.ndarray,*,
                  tile:int=512,check_active:Callable[[],None]=lambda:None)->bool:
    """Reconstruct exact tiles from original vectors; return verified nonzero flag.

    This worker check is not a backend admission or qualification certificate.
    It preserves the existing analytic stroke/polygon rasterization algorithm.
    """
    check_active();mask.coherent()
    shape=(mask.canvas.height,mask.canvas.width)
    for array in (target,protected):
        if array.shape!=shape or array.dtype!=np.dtype('float32') or not array.flags.c_contiguous or array.flags.writeable:
            raise ValueError('INVALID_RASTER_OUTPUT')
    nonzero=False
    for part in rasterize(mask,source_sha256,tile_size=tile,check_active=check_active):
        check_active()
        h,w=part.target.shape;region=(slice(part.y,part.y+h),slice(part.x,part.x+w))
        if not np.array_equal(target[region],part.target) or not np.array_equal(protected[region],part.protected):
            raise ValueError('RASTER_VECTOR_MISMATCH')
        nonzero=nonzero or bool(np.any(part.protected>0))
    check_active()
    return nonzero

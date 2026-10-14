"""Full-resolution output statistics; HDR pixels are never clamped here."""
import numpy as np


def negative_clipping_fraction(rgb,*,check_active):
    if not callable(check_active) or not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[-1]!=3 or min(rgb.shape[:2])<1 or rgb.shape[0]*rgb.shape[1]>200_000_000:
        raise ValueError('NEGATIVE_STATISTICS_INVALID')
    clipped=0
    for y in range(0,rgb.shape[0],512):
        for x in range(0,rgb.shape[1],512):
            check_active();tile=rgb[y:y+512,x:x+512]
            if not np.isfinite(tile).all():raise ValueError('NEGATIVE_STATISTICS_INVALID')
            clipped+=int(np.count_nonzero(np.any((tile>=1)|(tile<0),axis=-1)))
            check_active()
    return clipped/(rgb.shape[0]*rgb.shape[1])

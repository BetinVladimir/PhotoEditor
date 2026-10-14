"""Owned final source restoration under verified positive protection tiles."""
from contextlib import contextmanager
import numpy as np
from .render_adjustments import AdjustmentPixels


@contextmanager
def preserve_render_protection(artifacts, original, final, read_protected, *, tile_size=512, check_active=lambda:None):
    """Restore original RGB/alpha bits; absent alpha means opaque one.

    Caller supplies verified source-bound mask tiles; generated coverage is
    retained for separately validated provenance. Never mutate borrowed inputs.
    """
    check_active()
    if read_protected is None:
        yield final
        check_active()
        return
    if (not callable(read_protected) or type(tile_size) is not int or tile_size not in (256,512,1024)
            or original.rgb.shape!=final.rgb.shape or original.rgb.dtype!=np.float32 or final.rgb.dtype!=np.float32):
        raise ValueError('RENDER_PROTECTION_INPUT_INVALID')
    if original.rgb.ndim!=3 or original.rgb.shape[2]!=3 or min(original.rgb.shape[:2])<1:
        raise ValueError("RENDER_PROTECTION_INPUT_INVALID")
    h,w,_=original.rgb.shape
    for value in (original.alpha,final.alpha):
        if value is not None and (not isinstance(value,np.ndarray) or value.dtype!=np.float32 or value.shape!=(h,w)):
            raise ValueError("RENDER_PROTECTION_ALPHA_INVALID")
    if h*w>200000000:raise ValueError('RENDER_PROTECTION_PIXEL_LIMIT')
    with artifacts.scratch.reserve(h*w*16+16384) as owner:
        rgb=np.lib.format.open_memmap(owner.path('protected-rgb','.npy'),mode='w+',dtype='<f4',shape=(h,w,3))
        alpha=None
        try:
            if original.alpha is not None or final.alpha is not None:
                alpha=np.lib.format.open_memmap(owner.path('protected-alpha','.npy'),mode='w+',dtype='<f4',shape=(h,w))
            for y in range(0,h,tile_size):
                for x in range(0,w,tile_size):
                    check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x);part=np.s_[y:y+th,x:x+tw]
                    mask=read_protected(x,y,tw,th)
                    if (not isinstance(mask,np.ndarray) or mask.dtype!=np.float32 or mask.shape!=(th,tw)
                            or not np.isfinite(mask).all() or np.any(mask<0) or np.any(mask>1)):
                        raise ValueError('RENDER_PROTECTION_MASK_INVALID')
                    if not np.isfinite(original.rgb[part]).all() or not np.isfinite(final.rgb[part]).all():
                        raise ValueError("RENDER_PROTECTION_NONFINITE")
                    for value in (original.alpha,final.alpha):
                        if value is not None and (not np.isfinite(value[part]).all() or np.any(value[part]<0) or np.any(value[part]>1)):
                            raise ValueError("RENDER_PROTECTION_ALPHA_INVALID")
                    protected=mask>0
                    rgb[part]=final.rgb[part];rgb[part][protected]=original.rgb[part][protected]
                    if alpha is not None:
                        alpha[part]=1 if final.alpha is None else final.alpha[part]
                        alpha[part][protected]=1 if original.alpha is None else original.alpha[part][protected]
                    owner.validate()
            rgb.flush();rgb.flags.writeable=False
            if alpha is not None:alpha.flush();alpha.flags.writeable=False
            check_active()
            yield AdjustmentPixels(rgb,alpha,final.generated,final.effects)
            check_active()
        finally:
            if alpha is not None:alpha._mmap.close()
            rgb._mmap.close()

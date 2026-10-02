"""Final same-canvas bit-exact change evidence after all composition stages."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import numpy as np

@dataclass(frozen=True)
class RenderChangedFile:
    path:Path
    reservation:object
    changed_pixels:int
    rgb_changed:bool
    alpha_changed:bool


@contextmanager
def prepare_render_changes(source,final,scratch,*,read_protected=None,tile_size:int=512,max_tile_bytes:int,check_active=lambda:None):
    """Compare final pixels to original, not a union of transient layer changes.

    Alpha absence means opaque one; identical alpha values do not claim semantic
    change solely because representation differs. Publication handles reference
    reuse/conversion separately. Geometry changes require separate evidence.
    """
    check_active()
    if not isinstance(source.rgb,np.ndarray):
        raise ValueError('RENDER_CHANGE_INPUT_INVALID')
    shape=source.rgb.shape
    if (not isinstance(source.rgb,np.ndarray) or source.rgb.dtype!=np.float32
            or len(shape)!=3 or shape[2]!=3 or min(shape[:2])<1 or shape[0]*shape[1]>200000000
            or not isinstance(final.rgb,np.ndarray) or final.rgb.dtype!=np.float32 or final.rgb.shape!=shape
            or type(tile_size) is not int or tile_size not in (256,512,1024)
            or type(max_tile_bytes) is not int or max_tile_bytes<min(tile_size,shape[0])*min(tile_size,shape[1])*64
            or read_protected is not None and not callable(read_protected)):
        raise ValueError('RENDER_CHANGE_INPUT_INVALID')
    for alpha in (source.alpha,final.alpha):
        if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.dtype!=np.float32 or alpha.shape!=shape[:2]):
            raise ValueError('RENDER_CHANGE_ALPHA_INVALID')
    h,w=shape[:2]
    with scratch.reserve(h*w*4+8192) as reservation:
        path=reservation.path('render-changed','.npy')
        mapped=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w));count=0;rgb_changed=False;alpha_changed=False
        try:
            for y in range(0,h,tile_size):
                for x in range(0,w,tile_size):
                    check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x);part=np.s_[y:y+th,x:x+tw]
                    before,after=source.rgb[part],final.rgb[part]
                    if not np.isfinite(before).all() or not np.isfinite(after).all():raise ValueError('RENDER_CHANGE_NONFINITE')
                    rgb=np.any(before.view(np.uint32)!=after.view(np.uint32),axis=-1)
                    alphas=[np.ones((th,tw),np.float32) if value is None else value[part] for value in (source.alpha,final.alpha)]
                    for value in alphas:
                        if not np.isfinite(value).all() or np.any(value<0) or np.any(value>1):raise ValueError('RENDER_CHANGE_ALPHA_INVALID')
                    alpha=alphas[0].view(np.uint32)!=alphas[1].view(np.uint32);changed=rgb|alpha
                    if read_protected is not None:
                        protection=read_protected(x,y,tw,th)
                        if protection is not None:
                            if (not isinstance(protection,np.ndarray) or protection.dtype!=np.float32 or protection.shape!=(th,tw)
                                    or not np.isfinite(protection).all() or np.any(protection<0) or np.any(protection>1)):
                                raise ValueError('RENDER_CHANGE_PROTECTION_INVALID')
                            if np.any(changed&(protection>0)):raise ValueError('RENDER_PROTECTION_PIXEL_CHANGED')
                    mapped[part]=changed.astype(np.float32);count+=int(np.count_nonzero(changed))
                    rgb_changed=rgb_changed or bool(np.any(rgb));alpha_changed=alpha_changed or bool(np.any(alpha));reservation.validate()
            mapped.flush()
        finally:mapped._mmap.close()
        check_active();reservation.validate_file(path)
        yield RenderChangedFile(path,reservation,count,rgb_changed,alpha_changed)
        check_active()

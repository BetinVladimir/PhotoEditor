"""Scoped same-canvas composition with final rather than transient evidence."""
from contextlib import contextmanager
from dataclasses import dataclass
import numpy as np
from .render_adjustments import AdjustmentPixels,compose_render_adjustments
from .render_changes import RenderChangedFile,prepare_render_changes


@dataclass(frozen=True)
class RenderComposition:
    pixels:AdjustmentPixels
    changes:RenderChangedFile


def _validate_inputs(original,base,tile_size,max_tile_bytes,check_active):
    if (not isinstance(original,AdjustmentPixels) or not isinstance(base,AdjustmentPixels)
            or not isinstance(original.rgb,np.ndarray) or original.rgb.dtype!=np.float32
            or original.rgb.ndim!=3 or original.rgb.shape[2]!=3
            or min(original.rgb.shape[:2])<1 or original.rgb.shape[0]*original.rgb.shape[1]>200000000
            or not isinstance(base.rgb,np.ndarray) or base.rgb.dtype!=np.float32 or base.rgb.shape!=original.rgb.shape
            or type(tile_size) is not int or tile_size not in (256,512,1024)
            or type(max_tile_bytes) is not int):
        raise ValueError('RENDER_COMPOSITION_INPUT_INVALID')
    h,w=original.rgb.shape[:2]
    if max_tile_bytes<min(h,tile_size)*min(w,tile_size)*256:
        raise ValueError('RENDER_COMPOSITION_TILE_BUDGET')
    for pixels in (original,base):
        for array in (pixels.alpha,pixels.generated):
            if array is not None and (not isinstance(array,np.ndarray) or array.dtype!=np.float32 or array.shape!=(h,w)):
                raise ValueError('RENDER_COMPOSITION_COVERAGE_INVALID')
        for y in range(0,h,tile_size):
            for x in range(0,w,tile_size):
                check_active();part=np.s_[y:y+tile_size,x:x+tile_size]
                if not np.isfinite(pixels.rgb[part]).all():raise ValueError('RENDER_COMPOSITION_NONFINITE')
                for array in (pixels.alpha,pixels.generated):
                    if array is not None:
                        tile=array[part]
                        if not np.isfinite(tile).all() or np.any(tile<0) or np.any(tile>1):
                            raise ValueError('RENDER_COMPOSITION_COVERAGE_INVALID')


@contextmanager
def compose_render_pixels(original,base,layers,masks,scratch,render_proposal,*,color,
                          max_tile_bytes:int,tile_size:int=512,check_active=lambda:None):
    """Keep final pixels and change evidence alive through caller staging.

    original is the admitted original source, base the fully prepared base-style
    proposal. Their canvas/color equivalence and source lineage are established
    upstream. Geometry is explicitly unsupported here. Changes compare original
    with the final composition, including base-style effects, and protection is
    checked again across the whole result. Caller owns both input lifetimes.
    """
    check_active()
    _validate_inputs(original,base,tile_size,max_tile_bytes,check_active)
    with compose_render_adjustments(base.rgb,base.alpha,layers,masks,scratch,render_proposal,
            color=color,max_tile_bytes=max_tile_bytes,tile_size=tile_size,
            generated=base.generated,check_active=check_active) as pixels:
        with prepare_render_changes(original,pixels,scratch,read_protected=masks.protection,
                max_tile_bytes=max_tile_bytes,tile_size=tile_size,check_active=check_active) as changes:
            yield RenderComposition(pixels,changes)
            check_active()

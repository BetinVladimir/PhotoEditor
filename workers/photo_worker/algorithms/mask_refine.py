"""Bounded guided refinement with the complete2r dependency halo."""
from collections.abc import Callable,Iterator
import numpy as np
from .tiles import guided_filter,plan_tiles
from .mask_transform import TransformedMaskTile
from .color import LUMA
from photo_worker.contracts.images import Rect


def refine_tiles(probability:np.ndarray,guide_rgb:np.ndarray,*,radius:int|None=None,tile_size:int=512,
                 max_work_pixels:int=4194304,valid_region:Rect|None=None,
                 check_active:Callable[[],None]=lambda:None)->Iterator[TransformedMaskTile]:
    """Use encoded work Rec2020 luma, eps1e-4, then clip probability[0,1].

    Both inputs are borrowed arrays/mappings in the same target canvas. Output
    cores are copied out of temporary halos. No full-frame guide is allocated.
    """
    check_active()
    if probability.ndim!=2 or probability.dtype!=np.float32:raise ValueError('Invalid probability layout')
    h,w=probability.shape
    if min(h,w)<1 or h*w>200000000 or guide_rgb.shape!=(h,w,3) or guide_rgb.dtype!=np.float32:
        raise ValueError('Invalid guide layout')
    if radius is None:radius=max(1,round(8*max(h,w)/1024))
    if type(radius) is not int or radius<1 or type(tile_size) is not int or tile_size not in (256,512,1024):
        raise ValueError('Invalid guided filter parameters')
    if type(max_work_pixels) is not int or not 1<=max_work_pixels<=16777216:raise ValueError('Invalid working pixel budget')
    if min(h,tile_size+4*radius)*min(w,tile_size+4*radius)>max_work_pixels:
        raise ValueError('GUIDED_WORK_BUDGET_EXCEEDED')
    if valid_region is not None and (valid_region.x+valid_region.width>w or valid_region.y+valid_region.height>h):
        raise ValueError('Invalid output region')
    for row in range(h):
        for x in range(0,w,16384):
            check_active()
            for values in (probability[row,x:x+16384],guide_rgb[row,x:x+16384]):
                if not np.isfinite(values).all() or np.any(values<0) or np.any(values>1):
                    raise ValueError('Non-finite or out-of-range guided input')
    for tile in plan_tiles(w,h,tile_size,halo=2*radius):
        check_active()
        rgb=guide_rgb[tile.read.slices]
        guide=np.sum(rgb*LUMA.astype(np.float32),axis=-1,dtype=np.float32)
        refined=guided_filter(guide,probability[tile.read.slices],radius,1e-4)
        check_active()
        output=np.clip(refined[tile.core_in_read.slices],0,1).copy()
        if valid_region is not None:
            r=valid_region;x,y=tile.core.x,tile.core.y
            xs=np.arange(x,x+tile.core.width)[None,:];ys=np.arange(y,y+tile.core.height)[:,None]
            output[(xs<r.x)|(xs>=r.x+r.width)|(ys<r.y)|(ys>=r.y+r.height)]=0
        output.flags.writeable=False
        yield TransformedMaskTile(tile.core.x,tile.core.y,output)

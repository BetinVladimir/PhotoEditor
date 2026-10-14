"""Bounded proposal tiles with global clothing scale and Gaussian support halo."""
import math
import numpy as np

from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
from .portrait_local import hair_proposal, clothing_proposal


def local_proposal_tiles(linear, params, *, tile=512, check_active=lambda:None):
    if (not isinstance(linear,np.ndarray) or linear.dtype!=np.float32 or
            linear.ndim!=3 or linear.shape[-1]!=3 or min(linear.shape[:2])<1 or
            not isinstance(params,(PortraitHairParams,PortraitClothingParams)) or
            type(tile) is not int or not 1<=tile<=1024):
        raise ValueError('INPUT_INVALID')
    h,w=linear.shape[:2]
    sigma=.01*min(h,w)
    # OpenCV's automatic Gaussian support is less than this conservative halo.
    # At global edges the crop edge is the image edge, retaining reflect101.
    halo=math.ceil(8*max(1.2,sigma if isinstance(params,PortraitClothingParams) else 1.2))+2
    for y in range(0,h,tile):
        for x in range(0,w,tile):
            check_active()
            y1,x1=min(h,y+tile),min(w,x+tile)
            top,left=max(0,y-halo),max(0,x-halo)
            bottom,right=min(h,y1+halo),min(w,x1+halo)
            block=linear[top:bottom,left:right]
            if isinstance(params,PortraitHairParams):
                proposal=hair_proposal(block,params.hue_delta_degrees,params.saturation_factor,params.detail)
            else:
                proposal=clothing_proposal(block,params.hue_delta_degrees,params.saturation_factor,
                                           params.wrinkle_strength,coarse_sigma=sigma)
            check_active()
            yield (y,y1,x,x1),proposal[y-top:y1-top,x-left:x1-left].copy()

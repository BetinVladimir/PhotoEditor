"""Tile-invariant pair warping with linear-light bilinear interpolation."""
from __future__ import annotations
from dataclasses import dataclass
from typing import Callable,Iterator
import numpy as np
from .color import to_linear,to_encoded
from .registration import PairAlignment,_similarity,_image

WARP_VERSION='linear_bilinear_global_v1'

@dataclass(frozen=True)
class WarpedTile:
    x:int
    y:int
    rgb:np.ndarray
    valid:np.ndarray


def validate_shape(shape:tuple[int,int]):
    if (len(shape)!=2 or any(type(v) is not int or v<1 for v in shape)
        or shape[0]*shape[1]>200000000):raise ValueError('PAIR_SHAPE_INVALID')


def warp_pair_tiles(before:np.ndarray,alignment:PairAlignment,target_shape:tuple[int,int],*,
                    tile:int=512,check_active:Callable[[],None]=lambda:None)->Iterator[WarpedTile]:
    """Yield encoded Rec.2020 float32 RGB and eroded full-support validity.

    OpenCV similarity matrices use integer pixel-centre coordinates. Compute
    inverse coordinates globally in float64, not relative to tile origins.
    Gather only four neighbours per output pixel; never decode the whole source
    into a linear RGB copy. Caller must consume and release tiles incrementally.
    """
    validate_shape(target_shape)
    if type(tile) is not int or tile not in (256,512,1024):raise ValueError('TILE_SIZE_INVALID')
    if not isinstance(before,np.ndarray) or before.dtype!=np.float32:raise ValueError('PAIR_IMAGE_INVALID')
    image=_image(before,check_active)
    source_h,source_w=image.shape[:2];height,width=target_shape
    inverse=np.linalg.inv(_similarity(alignment.before_to_after))
    check_active()
    def coordinates(xs,ys):
        return (inverse[0,0]*xs+inverse[0,1]*ys+inverse[0,2],
                inverse[1,0]*xs+inverse[1,1]*ys+inverse[1,2])
    for y in range(0,height,tile):
        for x in range(0,width,tile):
            check_active()
            h,w=min(tile,height-y),min(tile,width-x)
            # One target-pixel halo permits validity erosion without tile seams.
            xs=np.arange(x-1,x+w+1,dtype=np.float64)[None,:]
            ys=np.arange(y-1,y+h+1,dtype=np.float64)[:,None]
            sx,sy=coordinates(xs,ys)
            support=(sx>=0)&(sx<=source_w-1)&(sy>=0)&(sy<=source_h-1)
            support&=(xs>=0)&(xs<width)&(ys>=0)&(ys<height)
            valid=np.ones((h,w),bool)
            for dy in range(3):
                for dx in range(3):valid&=support[dy:dy+h,dx:dx+w]
            sx,sy=sx[1:-1,1:-1],sy[1:-1,1:-1]
            # Very distant coordinates are saturated before integer conversion;
            # they contribute zero, while avoiding overflow in adversarial shifts.
            sx=np.clip(sx,-2,source_w+1);sy=np.clip(sy,-2,source_h+1)
            ix=np.floor(sx).astype(np.int64);iy=np.floor(sy).astype(np.int64)
            fx=sx-ix;fy=sy-iy
            linear=np.zeros((h,w,3),np.float32)
            for dy in (0,1):
                for dx in (0,1):
                    check_active()
                    xx,yy=ix+dx,iy+dy
                    inside=(xx>=0)&(xx<source_w)&(yy>=0)&(yy<source_h)
                    weight=((fx if dx else 1-fx)*(fy if dy else 1-fy)*inside).astype(np.float32)
                    pixels=image[np.clip(yy,0,source_h-1),np.clip(xx,0,source_w-1)]
                    linear+=to_linear(pixels)*weight[...,None]
            rgb=to_encoded(linear)
            rgb.flags.writeable=False;valid.flags.writeable=False
            check_active()
            yield WarpedTile(x,y,rgb,valid)

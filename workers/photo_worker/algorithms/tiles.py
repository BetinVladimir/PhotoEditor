from __future__ import annotations
from dataclasses import dataclass
from collections.abc import Iterator
import numpy as np
import cv2


@dataclass(frozen=True)
class Rect:
    x: int
    y: int
    width: int
    height: int

    @property
    def slices(self):
        return (slice(self.y,self.y+self.height),slice(self.x,self.x+self.width))


@dataclass(frozen=True)
class Tile:
    core: Rect
    read: Rect

    @property
    def core_in_read(self):
        return Rect(self.core.x-self.read.x,self.core.y-self.read.y,self.core.width,self.core.height)


def plan_tiles(width: int,height: int,tile_size: int,halo: int=0) -> Iterator[Tile]:
    if min(width,height,tile_size)<1 or halo<0:
        raise ValueError("Invalid tile dimensions")
    for y in range(0,height,tile_size):
        for x in range(0,width,tile_size):
            core=Rect(x,y,min(tile_size,width-x),min(tile_size,height-y))
            rx,ry=max(0,x-halo),max(0,y-halo)
            read=Rect(rx,ry,min(width,x+core.width+halo)-rx,min(height,y+core.height+halo)-ry)
            yield Tile(core,read)


def sample_map(array: np.ndarray,region: Rect,full_width: int,full_height: int) -> np.ndarray:
    if min(full_width,full_height)<1 or region.width<1 or region.height<1:
        raise ValueError("Invalid map dimensions")
    if region.x<0 or region.y<0 or region.x+region.width>full_width or region.y+region.height>full_height:
        raise ValueError("Region outside canvas")
    ph,pw=array.shape[:2]
    xs=(np.arange(region.x,region.x+region.width,dtype=np.float32)+.5)*pw/full_width-.5
    ys=(np.arange(region.y,region.y+region.height,dtype=np.float32)+.5)*ph/full_height-.5
    xx,yy=np.meshgrid(xs,ys)
    return cv2.remap(array.astype(np.float32),xx,yy,cv2.INTER_LINEAR,borderMode=cv2.BORDER_REPLICATE)


def guided_filter(guide: np.ndarray,signal: np.ndarray,radius: int,epsilon: float) -> np.ndarray:
    if guide.ndim!=2 or signal.shape[:2]!=guide.shape or radius<1 or epsilon<=0:
        raise ValueError("Invalid guided filter inputs")
    i=np.asarray(guide,dtype=np.float64)
    p=np.asarray(signal,dtype=np.float64)
    if not np.isfinite(i).all() or not np.isfinite(p).all():
        raise ValueError("Non-finite guided filter input")
    k=(2*radius+1,)*2
    def mean(x):return cv2.boxFilter(x,-1,k,borderType=cv2.BORDER_REFLECT_101)
    mi=mean(i);mp=mean(p);variance=mean(i*i)-mi*mi
    if p.ndim==3:
        i=i[...,None];mi=mi[...,None];variance=variance[...,None]
    covariance=mean(i*p)-mi*mp
    a=covariance/(variance+epsilon);b=mp-a*mi
    return (mean(a)*i+mean(b)).astype(np.float32)

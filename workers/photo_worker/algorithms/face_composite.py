"""Permutation-stable accumulation of face corrections from one immutable base."""
from dataclasses import dataclass
from collections.abc import Callable,Iterator,Sequence
from uuid import UUID
import numpy as np
from photo_worker.contracts.images import Rect

@dataclass(frozen=True)
class FaceCorrection:
    object_id:UUID
    roi:Rect
    delta:np.ndarray  # result-base, already multiplied by the effective skin mask
    weight:np.ndarray  # that same effective skin mask, including protection once

@dataclass(frozen=True)
class CompositeTile:
    x:int
    y:int
    rgb:np.ndarray


def composite_faces(source:np.ndarray,corrections:Sequence[FaceCorrection],*,tile_size:int=512,
                    check_active:Callable[[],None]=lambda:None)->Iterator[CompositeTile]:
    """output=base+sum(weighted_delta)/max(1,sum(weight)).

    All patches must be computed independently from source using the same global
    protection union. Deltas must be zero at zero weight. Borrowed patch arrays
    can be scratch memmaps; this function never stacks complete face ROIs.
    """
    check_active()
    if source.ndim!=3 or source.shape[2]!=3 or source.dtype!=np.float32 or min(source.shape[:2])<1 or source.shape[0]*source.shape[1]>200000000:
        raise ValueError('Invalid composite source')
    if type(tile_size) is not int or tile_size not in (256,512,1024) or len(corrections)>64:
        raise ValueError('Invalid composite limits')
    if any(not isinstance(p.object_id,UUID) for p in corrections) or len({p.object_id for p in corrections})!=len(corrections):
        raise ValueError('Invalid or duplicate face identity')
    h,w=source.shape[:2]
    ordered=sorted(corrections,key=lambda p:p.object_id.hex)
    for patch in ordered:
        r=patch.roi
        if r.x+r.width>w or r.y+r.height>h or patch.delta.shape!=(r.height,r.width,3) or patch.weight.shape!=(r.height,r.width) or patch.delta.dtype!=np.float32 or patch.weight.dtype!=np.float32:
            raise ValueError('Invalid face correction layout')
        for y in range(r.height):
            for x in range(0,r.width,16384):
                check_active();delta=patch.delta[y,x:x+16384];weight=patch.weight[y,x:x+16384]
                if not np.isfinite(delta).all() or not np.isfinite(weight).all() or np.any(weight<0) or np.any(weight>1):
                    raise ValueError('Invalid correction values')
                if np.any(np.abs(delta)>weight[...,None]+1e-6):raise ValueError('Correction exceeds its mask weight')
                if np.any(delta[weight==0]!=0):raise ValueError('Correction leaks outside mask')
    for y in range(0,h,tile_size):
        for x in range(0,w,tile_size):
            check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x)
            base=source[y:y+th,x:x+tw]
            if not np.isfinite(base).all() or np.any(base<0) or np.any(base>1):raise ValueError('Invalid base values')
            delta=np.zeros((th,tw,3),np.float64);weight=np.zeros((th,tw),np.float64)
            for patch in ordered:
                check_active();r=patch.roi
                x0,y0=max(x,r.x),max(y,r.y);x1,y1=min(x+tw,r.x+r.width),min(y+th,r.y+r.height)
                if x0>=x1 or y0>=y1:continue
                destination=(slice(y0-y,y1-y),slice(x0-x,x1-x))
                part=(slice(y0-r.y,y1-r.y),slice(x0-r.x,x1-r.x))
                delta[destination]+=patch.delta[part];weight[destination]+=patch.weight[part]
            output=np.clip(base+delta/np.maximum(1,weight)[...,None],0,1).astype(np.float32)
            output[weight==0]=base[weight==0]
            output.flags.writeable=False
            yield CompositeTile(x,y,output)

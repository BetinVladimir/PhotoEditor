"""Exact Stats20 using bounded RGB blocks and caller-owned luma workspace."""
from typing import Callable
import numpy as np
from .adaptation import Stats20,coverage_indices
from .color import LUMA,to_linear


def compute_stats20_bounded(rgb:np.ndarray,person:np.ndarray,luma_workspace:np.ndarray,*,
                            valid:np.ndarray|None=None,alpha:np.ndarray|None=None,encoding:str='linear',
                            check_active:Callable[[],None]=lambda:None)->Stats20:
    """Workspace is mutable float64 C-order length>=pixels, preferably memmap.

    Quantiles use exact NumPy linear interpolation on an in-place partition.
    Caller owns workspace lifetime and disk reservation. Never pass an image or
    input mask as workspace: inputs must stay immutable throughout both passes.
    """
    if (not isinstance(rgb,np.ndarray) or rgb.ndim!=3 or rgb.shape[2]!=3
        or min(rgb.shape[:2])<1 or rgb.dtype not in (np.dtype('float32'),np.dtype('float64'))
        or rgb.shape[0]*rgb.shape[1]>200000000 or encoding not in ('linear','gamma24')):
        raise ValueError('STATS_INPUT_INVALID')
    h,w=rgb.shape[:2]
    if not isinstance(person,np.ndarray) or person.shape!=(h,w) or person.dtype.kind not in 'fbui':raise ValueError('STATS_MASK_INVALID')
    if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.shape!=(h,w) or alpha.dtype not in (np.dtype('uint16'),np.dtype('float32'))):raise ValueError('STATS_ALPHA_INVALID')
    if valid is not None and (not isinstance(valid,np.ndarray) or valid.shape!=(h,w) or valid.dtype!=np.bool_):raise ValueError('STATS_VALID_INVALID')
    if (not isinstance(luma_workspace,np.ndarray) or luma_workspace.dtype!=np.float64 or luma_workspace.ndim!=1
        or len(luma_workspace)<h*w or not luma_workspace.flags.c_contiguous or not luma_workspace.flags.writeable
        or any(np.shares_memory(luma_workspace,item) for item in (rgb,person,alpha) if item is not None)
        or valid is not None and np.shares_memory(luma_workspace,valid)):
        raise ValueError('STATS_WORKSPACE_INVALID')
    count=0;mean=np.zeros(4,np.float64);m2=np.zeros(4,np.float64)
    dark=bright=people=0
    quad_sum=np.zeros(4,np.float64);quad_count=np.zeros(4,np.int64)
    coverage=np.zeros(512,bool)
    def chunks():
        inputs=(rgb,person,valid,alpha)
        if all(item is None or item.flags.c_contiguous for item in inputs):
            flat_rgb=rgb.reshape(-1,3);flat_person=person.reshape(-1)
            flat_valid=valid.reshape(-1) if valid is not None else None
            flat_alpha=alpha.reshape(-1) if alpha is not None else None
            for offset in range(0,h*w,16384):
                stop=offset+16384
                yield offset,flat_rgb[offset:stop],flat_person[offset:stop],flat_valid[offset:stop] if flat_valid is not None else None,flat_alpha[offset:stop] if flat_alpha is not None else None
        else:
            for row in range(h):
                for col in range(0,w,16384):
                    stop=col+16384
                    yield row*w+col,rgb[row,col:stop],person[row,col:stop],valid[row,col:stop] if valid is not None else None,alpha[row,col:stop] if alpha is not None else None
    for offset,block,mask,valid_block,alpha_block in chunks():
        check_active()
        if not np.isfinite(block).all() or not np.isfinite(mask).all() or np.any(mask<0) or np.any(mask>1):
            raise ValueError('STATS_NONFINITE_OR_MASK_RANGE')
        keep=valid_block if valid_block is not None else np.ones(len(block),bool)
        if alpha_block is not None:
            if alpha_block.dtype==np.float32 and (not np.isfinite(alpha_block).all() or np.any((alpha_block<0)|(alpha_block>1))):raise ValueError('STATS_ALPHA_INVALID')
            keep=keep&(alpha_block>0)
        values=block[keep]
        if not len(values):continue
        if encoding=='gamma24':values=to_linear(values)
        values=np.asarray(values,np.float64)
        luminance=values@LUMA
        n=len(values)
        luma_workspace[count:count+n]=luminance
        features=np.column_stack((values,np.ptp(values,axis=1)))
        chunk_mean=features.mean(axis=0)
        delta=chunk_mean-mean
        m2+=np.square(features-chunk_mean).sum(axis=0)+delta*delta*count*n/(count+n)
        mean+=delta*n/(count+n)
        count+=n
        dark+=int(np.count_nonzero(luminance<.02));bright+=int(np.count_nonzero(luminance>.98))
        people+=int(np.count_nonzero(mask[keep]>=.5))
        coverage[coverage_indices(values)]=True
        indices=np.flatnonzero(keep)+offset
        quadrants=2*(indices//w>=h//2).astype(np.int64)+(indices%w>=w//2).astype(np.int64)
        for quadrant in range(4):
            selected=luminance[quadrants==quadrant]
            quad_sum[quadrant]+=selected.sum();quad_count[quadrant]+=len(selected)
    if not count:raise ValueError('EMPTY_VALID_REGION')
    check_active()
    positions=np.array([.05,.25,.5,.75,.95])*(count-1)
    lower=np.floor(positions).astype(np.int64);upper=np.ceil(positions).astype(np.int64)
    workspace=luma_workspace[:count]
    # partition is one bounded-memory C call; cancellation resumes afterwards.
    workspace.partition(np.unique(np.r_[lower,upper]))
    check_active()
    quantiles=workspace[lower]+(workspace[upper]-workspace[lower])*(positions-lower)
    y_mean=float(mean[:3]@LUMA)
    quadrants=np.divide(quad_sum,quad_count,out=np.full(4,y_mean),where=quad_count!=0)
    std=np.sqrt(np.maximum(m2/count,0))
    values=np.r_[quantiles,mean[:3],std[:3],mean[3],std[3],dark/count,bright/count,people/count,quadrants]
    return Stats20(values.astype(np.float64),coverage)


def coverage_counts_bounded(rgb:np.ndarray,*,alpha:np.ndarray|None=None,encoding:str='linear',
                            check_active:Callable[[],None]=lambda:None)->np.ndarray:
    """Exact population counts, not just occupied cells or sampled pixels."""
    if (not isinstance(rgb,np.ndarray) or rgb.ndim!=3 or rgb.shape[2]!=3
        or min(rgb.shape[:2])<1 or rgb.shape[0]*rgb.shape[1]>200000000
        or rgb.dtype not in (np.dtype('float32'),np.dtype('float64')) or encoding not in ('linear','gamma24')):
        raise ValueError('COVERAGE_INPUT_INVALID')
    h,w=rgb.shape[:2]
    if alpha is not None and (not isinstance(alpha,np.ndarray) or alpha.shape!=(h,w) or alpha.dtype not in (np.dtype('uint16'),np.dtype('float32'))):
        raise ValueError('COVERAGE_ALPHA_INVALID')
    counts=np.zeros(512,np.int64)
    def chunks():
        if rgb.flags.c_contiguous and (alpha is None or alpha.flags.c_contiguous):
            flat=rgb.reshape(-1,3);opacity=alpha.reshape(-1) if alpha is not None else None
            for start in range(0,len(flat),16384):
                yield flat[start:start+16384],opacity[start:start+16384] if opacity is not None else None
        else:
            for y in range(h):
                for x in range(0,w,16384):yield rgb[y,x:x+16384],alpha[y,x:x+16384] if alpha is not None else None
    for pixels,opacity in chunks():
        check_active()
        if not np.isfinite(pixels).all():raise ValueError('COVERAGE_NONFINITE')
        if opacity is not None:
            if opacity.dtype==np.float32 and (not np.isfinite(opacity).all() or np.any((opacity<0)|(opacity>1))):raise ValueError('COVERAGE_ALPHA_INVALID')
            pixels=pixels[opacity>0]
        if encoding=='gamma24':pixels=to_linear(pixels)
        counts+=np.bincount(coverage_indices(pixels),minlength=512)
    if not counts.sum():raise ValueError('EMPTY_VALID_REGION')
    check_active()
    counts.flags.writeable=False
    return counts

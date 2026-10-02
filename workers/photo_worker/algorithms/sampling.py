from __future__ import annotations
from dataclasses import dataclass
import hashlib
from typing import Callable
import numpy as np
from .color import LUMA,to_linear

@dataclass(frozen=True)
class FitSplit:
    train: np.ndarray
    holdout: np.ndarray
    holdout_blocks: tuple[int,...]
    digest: str


def split_blocks(valid_mask: np.ndarray, seed: int,*,exclude_mask:np.ndarray|None=None,train_out:np.ndarray|None=None,
                 holdout_out:np.ndarray|None=None,check_active:Callable[[],None]=lambda:None) -> FitSplit:
    check_active()
    valid=np.asarray(valid_mask,dtype=bool)
    if valid.ndim!=2 or not valid.any():
        raise ValueError('EMPTY_VALID_REGION')
    exclude=None if exclude_mask is None else np.asarray(exclude_mask)
    if exclude is not None and (exclude.shape!=valid.shape or exclude.dtype not in (np.dtype('float32'),np.dtype('bool'))):
        raise ValueError('INVALID_EXCLUDE_MASK')
    h,w=valid.shape
    ys=np.linspace(0,h,9,dtype=np.int64);xs=np.linspace(0,w,9,dtype=np.int64)
    blocks=[]
    for y in range(8):
        for x in range(8):
            check_active()
            region=valid[ys[y]:ys[y+1],xs[x]:xs[x+1]]
            if exclude is not None:region=region&(exclude[ys[y]:ys[y+1],xs[x]:xs[x+1]]<.5)
            count=int(region.sum())
            if count:blocks.append((y*8+x,count))
    if len(blocks)<2:
        raise ValueError('INSUFFICIENT_SPATIAL_COVERAGE')
    rng=np.random.default_rng(seed)
    order=rng.permutation(len(blocks))
    target=float(sum(count for _,count in blocks))*.2
    selected=[];area=0
    for index in order[:-1]:
        block,count=blocks[int(index)]
        if not selected or abs(area+count-target)<abs(area-target):
            selected.append(block);area+=count
        if area>=target:break
    if (train_out is None)!=(holdout_out is None):raise ValueError('SPLIT_OUTPUTS_INVALID')
    if train_out is None:
        train=np.empty(valid.shape,dtype=bool);holdout=np.empty(valid.shape,dtype=bool)
    else:
        train,holdout=train_out,holdout_out
        for output in (train,holdout):
            if (not isinstance(output,np.ndarray) or output.shape!=valid.shape or output.dtype!=np.bool_
                or not output.flags.writeable or not output.flags.c_contiguous or np.shares_memory(output,valid)):
                raise ValueError('SPLIT_OUTPUTS_INVALID')
        if np.shares_memory(train,holdout):raise ValueError('SPLIT_OUTPUTS_INVALID')
    holdout.fill(False)
    for block in selected:
        y,x=divmod(block,8)
        holdout[ys[y]:ys[y+1],xs[x]:xs[x+1]]=True
    # Bounded C-order slices also preserve packed-bit digest bytes for widths
    # that are not divisible by eight. No full-frame inverted mask or bit array.
    for start in range(0,valid.size,262144):
        check_active()
        stop=start+262144
        region=valid.flat[start:stop]
        if exclude is not None:region=region&(exclude.flat[start:stop]<.5)
        held=holdout.flat[start:stop]&region
        holdout.flat[start:stop]=held
        train.flat[start:stop]=region&~held
    digest_state=hashlib.sha256()
    for output in (train,holdout):
        for start in range(0,output.size,262144):
            check_active()
            digest_state.update(np.packbits(output.flat[start:start+262144],bitorder='little').tobytes())
    digest_state.update(np.asarray(valid.shape,dtype='<i8').tobytes())
    digest=digest_state.hexdigest()
    check_active()
    train.flags.writeable=False;holdout.flags.writeable=False
    return FitSplit(train,holdout,tuple(sorted(selected)),digest)

@dataclass(frozen=True)
class PixelSample:
    indices: np.ndarray
    digest: str


class _Ranks:
    """Map sampled population ranks back to indices without storing the population."""
    def __init__(self,ranks):
        self.order=np.argsort(ranks)
        self.ranks=np.asarray(ranks)[self.order]
        self.values=np.empty(len(ranks),dtype=np.int64)
        self.seen=0

    def feed(self,indices):
        left=np.searchsorted(self.ranks,self.seen)
        right=np.searchsorted(self.ranks,self.seen+len(indices))
        self.values[self.order[left:right]]=indices[self.ranks[left:right]-self.seen]
        self.seen+=len(indices)


def sample_pixels(linear_rgb: np.ndarray, mask: np.ndarray, seed: int, max_samples: int=65536,
                  *,encoding:str='linear',check_active:Callable[[],None]=lambda:None) -> PixelSample:
    if encoding not in ('linear','gamma24'):raise ValueError('SAMPLE_ENCODING_INVALID')
    rgb=np.asarray(linear_rgb)
    mask=np.asarray(mask)
    if (rgb.ndim!=3 or rgb.shape[-1]!=3 or mask.shape!=rgb.shape[:2]
        or type(max_samples) is not int or not 1<=max_samples<=65536):
        raise ValueError('INVALID_SAMPLE_INPUT')
    # Contiguous mappings use bounded flat views; other arrays use row slices
    # so no reshape silently copies a full frame. At most 16384 pixels/block.
    def chunks():
        width=rgb.shape[1]
        if rgb.flags.c_contiguous and mask.flags.c_contiguous:
            flat=rgb.reshape(-1,3);valid=mask.reshape(-1)
            for start in range(0,len(flat),16384):
                yield start,flat[start:start+16384],valid[start:start+16384]
        else:
            for y in range(rgb.shape[0]):
                for x in range(0,width,16384):
                    yield y*width+x,rgb[y,x:x+16384],mask[y,x:x+16384]
    def blocks(exclude=None,luma=False):
        for offset,values,valid in chunks():
            check_active()
            if not np.isfinite(values).all():raise ValueError('INVALID_SAMPLE_INPUT')
            local=np.flatnonzero(valid)
            indices=local+offset
            if exclude is not None and len(exclude):
                keep=~np.isin(indices,exclude)
                local,indices=local[keep],indices[keep]
            bins=None
            if luma:
                samples=values[local]
                if encoding=='gamma24':samples=to_linear(samples)
                luminance=samples@LUMA
                bins=np.minimum((np.clip(luminance,0,1)*16).astype(np.int32),15)
            yield indices,bins
    check_active()
    available=sum(len(indices) for indices,_ in blocks())
    if not available:raise ValueError('EMPTY_VALID_REGION')
    rng=np.random.default_rng(seed)
    count=min(max_samples,available)
    uniform=_Ranks(rng.choice(available,size=count//2,replace=False))
    for indices,_ in blocks():uniform.feed(indices)
    selected=[uniform.values]
    bin_counts=np.zeros(16,dtype=np.int64)
    for indices,bins in blocks(uniform.values,True):bin_counts+=np.bincount(bins,minlength=16)
    per_bin=(count-len(uniform.values))//16
    bins_selected=[]
    for population in bin_counts:
        # Match the original RNG call sequence, including zero-size draws.
        bins_selected.append(_Ranks(rng.choice(int(population),size=min(per_bin,int(population)),replace=False)) if population else _Ranks(np.empty(0,dtype=np.int64)))
    for indices,bins in blocks(uniform.values,True):
        for index,selection in enumerate(bins_selected):selection.feed(indices[bins==index])
    selected.extend(selection.values for selection in bins_selected if len(selection.values))
    indices=np.concatenate(selected)
    need=count-len(indices)
    if need:
        filler=_Ranks(rng.choice(available-len(indices),size=need,replace=False))
        for candidates,_ in blocks(indices):filler.feed(candidates)
        indices=np.concatenate([indices,filler.values])
    check_active()
    indices=indices[rng.permutation(len(indices))].astype(np.int64)
    digest=hashlib.sha256(indices.astype('<i8').tobytes()).hexdigest()
    indices.flags.writeable=False
    return PixelSample(indices,digest)

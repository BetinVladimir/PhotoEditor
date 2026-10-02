"""Attempt-owned intermediates for fitting; never hold a linear frame in RAM."""
from __future__ import annotations
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from typing import Callable,Iterator
import numpy as np
from photo_worker.algorithms.registration import PairAlignment
from photo_worker.algorithms.sampling import FitSplit,split_blocks
from photo_worker.algorithms.warp import warp_pair_tiles,validate_shape,WARP_VERSION
from photo_worker.runtime.scratch import ScratchSpace

@dataclass(frozen=True)
class WarpedPair:
    before:np.memmap
    valid:np.memmap
    algorithm_version:str=WARP_VERSION


@contextmanager
def warped_pair(before:np.ndarray,alignment:PairAlignment,target_shape:tuple[int,int],scratch:ScratchSpace,*,
                tile:int=512,check_active:Callable[[],None]=lambda:None)->Iterator[WarpedPair]:
    """Maps are borrowed until context exit. Never return them from a task.

    Reserve RGB float32 + boolean support and two bounded NPY headers before
    opening writers. Source artifacts remain in their own concurrent reservations.
    Exit closes mappings before releasing the reservation, including on cancel.
    """
    validate_shape(target_shape)
    if type(tile) is not int or tile not in (256,512,1024):raise ValueError('TILE_SIZE_INVALID')
    check_active()
    pixels=target_shape[0]*target_shape[1]
    with scratch.reserve(pixels*13+8192) as reservation:
        with ExitStack() as mappings:
            rgb_path=reservation.path('aligned-rgb','.npy')
            valid_path=reservation.path('aligned-valid','.npy')
            reservation.validate()
            rgb=np.lib.format.open_memmap(rgb_path,mode='w+',dtype='<f4',shape=(*target_shape,3))
            mappings.callback(rgb._mmap.close)
            valid=np.lib.format.open_memmap(valid_path,mode='w+',dtype=np.bool_,shape=target_shape)
            mappings.callback(valid._mmap.close)
            for part in warp_pair_tiles(before,alignment,target_shape,tile=tile,check_active=check_active):
                h,w=part.valid.shape
                rgb[part.y:part.y+h,part.x:part.x+w]=part.rgb
                valid[part.y:part.y+h,part.x:part.x+w]=part.valid
            check_active()
            rgb.flush();valid.flush()
            reservation.validate()
            rgb.flags.writeable=False;valid.flags.writeable=False
            check_active()
            yield WarpedPair(rgb,valid)


@contextmanager
def spatial_split(valid:np.ndarray,seed:int,scratch:ScratchSpace,*,
                  ignore:np.ndarray|None=None,
                  check_active:Callable[[],None]=lambda:None)->Iterator[FitSplit]:
    """Spatial masks share the same borrowed-map lifetime as warped_pair."""
    if not isinstance(valid,np.ndarray) or valid.dtype!=np.bool_ or valid.ndim!=2:
        raise ValueError('SPLIT_INPUT_INVALID')
    validate_shape(valid.shape)
    check_active()
    with scratch.reserve(valid.size*2+8192) as reservation:
        with ExitStack() as mappings:
            outputs=[]
            for role in ('train-mask','holdout-mask'):
                path=reservation.path(role,'.npy')
                output=np.lib.format.open_memmap(path,mode='w+',dtype=np.bool_,shape=valid.shape)
                mappings.callback(output._mmap.close)
                outputs.append(output)
            split=split_blocks(valid,seed,exclude_mask=ignore,train_out=outputs[0],holdout_out=outputs[1],check_active=check_active)
            for output in outputs:output.flush()
            reservation.validate()
            check_active()
            yield split


@dataclass(frozen=True)
class SampleEvidence:
    split_digest:str
    holdout_blocks:tuple[int,...]
    train_digest:str
    holdout_digest:str
    train_seed:int
    holdout_seed:int
    warp_version:str=WARP_VERSION


def prepare_samples(pair_id:str,before:np.ndarray,after:np.ndarray,alignment:PairAlignment,scratch:ScratchSpace,*,
                    person:np.ndarray|None=None,ignore:np.ndarray|None=None,seed:int=42,max_samples:int=65536,tile:int=512,
                    check_active:Callable[[],None]=lambda:None):
    """Copy only bounded samples out of temporary warp/split mappings.

    person, when supplied, is already on the AFTER canvas. This helper does not
    invent masks or warp a mask whose coordinate provenance has not been checked
    by the task adapter. Returned evidence pins split and both sampled index sets.
    """
    from photo_worker.algorithms.fitting import PairSamples
    from photo_worker.algorithms.sampling import sample_pixels
    from photo_worker.algorithms.registration import _image
    if not isinstance(pair_id,str) or not pair_id or len(pair_id)>120:raise ValueError('PAIR_ID_INVALID')
    if type(seed) is not int or not 0<=seed<=2**32-1:raise ValueError('SAMPLE_SEED_INVALID')
    if type(max_samples) is not int or not 1<=max_samples<=65536:raise ValueError('INVALID_SAMPLE_INPUT')
    if not isinstance(after,np.ndarray) or after.dtype!=np.float32:raise ValueError('PAIR_IMAGE_INVALID')
    validate_shape(after.shape[:2]);_image(after,check_active)
    if person is not None:
        if not isinstance(person,np.ndarray) or person.shape!=after.shape[:2] or person.dtype!=np.float32:
            raise ValueError('FIT_MASK_INVALID')
        for y in range(person.shape[0]):
            for x in range(0,person.shape[1],16384):
                check_active();block=person[y,x:x+16384]
                if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):raise ValueError('FIT_MASK_INVALID')
    if ignore is not None:
        if not isinstance(ignore,np.ndarray) or ignore.shape!=after.shape[:2] or ignore.dtype!=np.float32:
            raise ValueError('FIT_IGNORE_INVALID')
        for y in range(ignore.shape[0]):
            for x in range(0,ignore.shape[1],16384):
                check_active();block=ignore[y,x:x+16384]
                if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):raise ValueError('FIT_IGNORE_INVALID')
    holdout_seed=(seed+1)%(2**32)
    with warped_pair(before,alignment,after.shape[:2],scratch,tile=tile,check_active=check_active) as warped:
        with spatial_split(warped.valid,seed,scratch,ignore=ignore,check_active=check_active) as split:
            train=sample_pixels(warped.before,split.train,seed,max_samples,encoding='gamma24',check_active=check_active)
            held=sample_pixels(warped.before,split.holdout,holdout_seed,max_samples,encoding='gamma24',check_active=check_active)
            def gather(array,indices):
                y,x=np.divmod(indices,after.shape[1])
                return np.array(array[y,x],dtype=np.float32,copy=True)
            check_active()
            samples=PairSamples(pair_id,gather(warped.before,train.indices),gather(after,train.indices),
                gather(warped.before,held.indices),gather(after,held.indices),
                gather(person,train.indices) if person is not None else None,
                gather(person,held.indices) if person is not None else None)
            samples.validate()
            evidence=SampleEvidence(split.digest,split.holdout_blocks,train.digest,held.digest,seed,holdout_seed)
    check_active()
    return samples,evidence

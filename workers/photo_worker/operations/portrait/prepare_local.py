"""Attempt-scoped local portrait pixels; no staging or artifact authority."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from photo_worker.algorithms.portrait_local import compose_local
from photo_worker.algorithms.portrait_local_tiles import local_proposal_tiles
from photo_worker.contracts.portrait_local import PortraitHairParams, PortraitClothingParams
from photo_worker.operations.optical.color import to_working_linear, from_working_linear
from .local_resources import admit_local_portrait


@dataclass(frozen=True)
class LocalPortraitFiles:
    rgb: Path
    changed_mask: Path
    reservation: object
    review_required: bool


@contextmanager
def prepare_local(params, rgb, target, protected, color, scratch, profile,
                  *, check_active=lambda: None):
    if not isinstance(params,(PortraitHairParams,PortraitClothingParams)):
        raise ValueError('PORTRAIT_PARAMS_INVALID')
    if not isinstance(rgb,np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[-1] != 3:
        raise ValueError('PORTRAIT_SOURCE_INVALID')
    h,w = rgb.shape[:2]
    admit_local_portrait(profile,w,h)
    for mask in (target,protected):
        if not isinstance(mask,np.ndarray) or mask.dtype != np.float32 or mask.shape != (h,w):
            raise ValueError('PORTRAIT_MASK_INVALID')
    check_active()
    # Validate the transfer contract even in the exact zero-strength branch.
    if color.encoding != 'linear' and not (color.encoding=='gamma24' and color.profile_id=='work_rec2020_gamma24_v1'):
        raise ValueError('PORTRAIT_COLOR_UNSUPPORTED')
    # Validate in bounded blocks too; full-image boolean arrays would defeat
    # the memory bound before proposal processing begins.
    for y in range(0,h,512):
        for x in range(0,w,512):
            check_active()
            if not np.isfinite(rgb[y:y+512,x:x+512]).all():
                raise ValueError('PORTRAIT_SOURCE_INVALID')
            for mask in (target,protected):
                block=mask[y:y+512,x:x+512]
                if not np.isfinite(block).all() or np.any((block<0)|(block>1)):
                    raise ValueError('PORTRAIT_MASK_INVALID')
    with scratch.reserve(28*w*h+32768) as reservation:
        out = reservation.path('portrait-local-rgb','.npy')
        changed = reservation.path('portrait-local-changed','.npy')
        output = np.lib.format.open_memmap(out,mode='w+',dtype='<f4',shape=(h,w,3))
        changes = None
        linear = None
        try:
            changes = np.lib.format.open_memmap(changed,mode='w+',dtype='<f4',shape=(h,w))
            if params.strength > 0:
                linear_path = reservation.path('portrait-local-linear','.npy')
                linear = np.lib.format.open_memmap(linear_path,mode='w+',dtype='<f4',shape=(h,w,3))
                for y in range(0,h,512):
                    for x in range(0,w,512):
                        check_active()
                        linear[y:y+512,x:x+512] = to_working_linear(rgb[y:y+512,x:x+512],color)
                linear.flush()
                for (y,y1,x,x1),proposal in local_proposal_tiles(linear,params,check_active=check_active):
                    base = rgb[y:y1,x:x1]
                    mask = target[y:y1,x:x1]
                    guard = protected[y:y1,x:x1]
                    hard_guard = (guard>0).astype(np.float32)
                    encoded = from_working_linear(compose_local(linear[y:y1,x:x1],proposal,mask,hard_guard,params.strength),color)
                    untouched = (mask==0)|(guard>0)
                    encoded[untouched] = base[untouched]
                    check_active()
                    output[y:y1,x:x1] = encoded
                    changes[y:y1,x:x1] = np.any(encoded!=base,axis=-1).astype(np.float32)
            else:
                for y in range(0,h,512):
                    for x in range(0,w,512):
                        check_active()
                        output[y:y+512,x:x+512] = rgb[y:y+512,x:x+512]
                        changes[y:y+512,x:x+512] = 0
            output.flush();changes.flush()
        finally:
            for array in (linear,changes,output):
                if array is not None: array._mmap.close()
        reservation.validate()
        check_active()
        yield LocalPortraitFiles(out,changed,reservation,
            isinstance(params,PortraitClothingParams) and params.strength>0 and params.wrinkle_strength>0)

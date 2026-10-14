"""Attempt-scoped V28 relight preparation; no artifact or admission authority."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.relight import build_light_field,relight_proposal
from photo_worker.algorithms.portrait_local import compose_local
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.optical.color import to_working_linear,from_working_linear
from .local_resources import admit_local_portrait


@dataclass(frozen=True)
class RelightFiles:
    rgb:Path
    changed_mask:Path
    reservation:object
    review_required:bool=True


@contextmanager
def prepare_relight(params,rgb,depth,invalid,subject,protected,color,scratch,profile,*,depth_sigma,check_active=lambda:None):
    """Depth sigma must come from separately authorized calibration.

    Retains the conservative local portrait resource ceiling until relight
    device qualification exists. This does not enable the operation registry.
    """
    check_active()
    if not isinstance(params,ImageRelightParams):raise ValueError('RELIGHT_PARAMS_INVALID')
    if not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[-1]!=3:
        raise ValueError('RELIGHT_SOURCE_INVALID')
    h,w=rgb.shape[:2];admit_local_portrait(profile,w,h)
    if not isinstance(depth,np.ndarray) or depth.shape!=(h,w):raise ValueError('RELIGHT_DEPTH_INVALID')
    for mask in (subject,protected):
        if not isinstance(mask,np.ndarray) or mask.dtype!=np.float32 or mask.shape!=(h,w):raise ValueError('RELIGHT_MASK_INVALID')
    if color.encoding!='linear' and not (color.encoding=='gamma24' and color.profile_id=='work_rec2020_gamma24_v1'):
        raise ValueError('RELIGHT_COLOR_UNSUPPORTED')
    for y in range(0,h,256):
        for x in range(0,w,512):
            check_active()
            if not np.isfinite(rgb[y:y+256,x:x+512]).all():raise ValueError('RELIGHT_SOURCE_INVALID')
            for mask in (subject,protected):
                block=mask[y:y+256,x:x+512]
                if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):raise ValueError('RELIGHT_MASK_INVALID')
    with scratch.reserve(20*w*h+32768) as reservation:
        rgb_path=reservation.path('relight-rgb','.npy')
        changed_path=reservation.path('relight-changed','.npy')
        field_path=reservation.path('relight-field','.npy')
        arrays=[]
        try:
            output=np.lib.format.open_memmap(rgb_path,mode='w+',dtype='<f4',shape=(h,w,3));arrays.append(output)
            changed=np.lib.format.open_memmap(changed_path,mode='w+',dtype='<f4',shape=(h,w));arrays.append(changed)
            field=np.lib.format.open_memmap(field_path,mode='w+',dtype='<f4',shape=(h,w));arrays.append(field)
            build_light_field(depth,invalid,params.lights,depth_sigma,out=field,check_active=check_active)
            for y in range(0,h,256):
                for x in range(0,w,512):
                    check_active();region=np.s_[y:y+256,x:x+512]
                    base=rgb[region];mask=subject[region];guard=protected[region];ev=field[region]
                    if params.strength==0:
                        encoded=base.copy()
                    else:
                        linear=to_working_linear(base,color)
                        proposal=relight_proposal(linear,ev)
                        # V2 safe-delta contract forbids edits at every positive
                        # protected sample, including antialiased mask edges.
                        hard_guard=(guard>0).astype(np.float32)
                        encoded=from_working_linear(compose_local(linear,proposal,mask,hard_guard,params.strength),color)
                        untouched=(mask==0)|(guard>0)|(ev==0)
                        encoded[untouched]=base[untouched]
                    check_active()
                    output[region]=encoded
                    changed[region]=np.any(encoded!=base,axis=-1).astype(np.float32)
            for array in arrays:array.flush()
        finally:
            for array in reversed(arrays):array._mmap.close()
        reservation.validate();check_active()
        yield RelightFiles(rgb_path,changed_path,reservation)

"""Attempt-scoped RGB and changed-mask files for one admitted donor patch."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import numpy as np


@dataclass(frozen=True)
class DonorPortraitFiles:
    rgb: Path
    changed_mask: Path
    reservation: object
    review_required: bool = True


@contextmanager
def write_donor_patch_files(source_rgb, patch, origin, scratch, *, target, protected, check_active):
    """Copy unchanged canvas in bounded tiles; overlay a checked patch exactly.

    Caller owns source/mask/patch authority, halo/core selection and alpha ref.
    Does not perform admission, publish files or compose independently tiled
    donor statistics. Reservation remains alive until the caller stages files.
    """
    source=np.asarray(source_rgb)
    pixels=np.asarray(patch.pixels)
    changes=np.asarray(patch.changed_mask)
    if (not callable(check_active) or source.dtype!=np.float32 or source.ndim!=3 or
            source.shape[2]!=3 or min(source.shape[:2])<1 or
            source.shape[0]*source.shape[1]>200_000_000 or pixels.dtype!=np.float32 or
            pixels.ndim!=3 or pixels.shape[2]!=3 or min(pixels.shape[:2])<1 or
            pixels.shape[0]*pixels.shape[1]>1_048_576 or changes.dtype!=np.float32 or
            changes.shape!=pixels.shape[:2] or not isinstance(origin,tuple) or len(origin)!=2 or
            any(type(v) is not int or v<0 for v in origin)):
        raise ValueError('DONOR_OUTPUT_INVALID')
    x,y=origin;h,w=source.shape[:2];ph,pw=pixels.shape[:2]
    if x+pw>w or y+ph>h or not np.isfinite(pixels).all():
        raise ValueError('DONOR_OUTPUT_INVALID')
    expected=np.any(pixels.view(np.uint32)!=source[y:y+ph,x:x+pw].view(np.uint32),axis=2).astype(np.float32)
    if not np.array_equal(expected,changes):
        raise ValueError('DONOR_CHANGED_MASK')
    for value in (target,protected):
        if (not isinstance(value,np.ndarray) or value.dtype!=np.float32 or
                value.shape!=(ph,pw) or not np.isfinite(value).all() or
                np.any(value<0) or np.any(value>1)):
            raise ValueError('DONOR_OUTPUT_MASK')
    if np.any((expected>0)&((target==0)|(protected>0))):
        raise ValueError('DONOR_OUTSIDE_MASK')
    check_active()
    with scratch.reserve(16*w*h+32768) as reservation:
        rgb_path=reservation.path('portrait-donor-rgb','.npy')
        changed_path=reservation.path('portrait-donor-changed','.npy')
        output=mask=None
        try:
            output=np.lib.format.open_memmap(rgb_path,mode='w+',dtype='<f4',shape=source.shape)
            mask=np.lib.format.open_memmap(changed_path,mode='w+',dtype='<f4',shape=(h,w))
            for ty in range(0,h,512):
                for tx in range(0,w,512):
                    check_active()
                    block=source[ty:ty+512,tx:tx+512]
                    if not np.isfinite(block).all():raise ValueError('DONOR_OUTPUT_INVALID')
                    output[ty:ty+512,tx:tx+512]=block
                    mask[ty:ty+512,tx:tx+512]=0
            check_active()
            output[y:y+ph,x:x+pw]=pixels
            mask[y:y+ph,x:x+pw]=changes
            output.flush();mask.flush()
        finally:
            for array in (mask,output):
                if array is not None:array._mmap.close()
        reservation.validate();check_active()
        yield DonorPortraitFiles(rgb_path,changed_path,reservation)

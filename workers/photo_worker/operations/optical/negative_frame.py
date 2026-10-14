"""Tiled linear negative conversion into caller-owned unpublished mappings.

Caller retains mappings, admits their resources, and discards partial output on
failure. This module does not publish or grant source/qualification authority.
"""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from .negative_calibration import verify_negative_calibration
from .negative import invert_negative_tile


def convert_negative_frame(scan, output, params, calibration_raw, *, input_icc_sha,
        epsilon, check_active, ram_max_bytes, alpha=None, output_alpha=None):
    if not callable(check_active):raise ValueError('NEGATIVE_ACTIVITY_REQUIRED')
    p=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    c=verify_negative_calibration(p,calibration_raw,input_icc_sha=input_icc_sha)
    if (not isinstance(scan,np.ndarray) or not isinstance(output,np.ndarray)
            or scan.dtype!=np.float32 or output.dtype!=np.float32 or scan.ndim!=3
            or scan.shape[-1]!=3 or min(scan.shape[:2])<1
            or scan.shape[0]*scan.shape[1]>200_000_000 or output.shape!=scan.shape
            or not output.flags.writeable or np.shares_memory(scan,output)):
        raise ValueError('NEGATIVE_FRAME_INVALID')
    if (alpha is None)!=(output_alpha is None):raise ValueError('NEGATIVE_ALPHA_INVALID')
    if alpha is not None:
        if (not isinstance(alpha,np.ndarray) or not isinstance(output_alpha,np.ndarray)
                or alpha.dtype!=np.float32 or output_alpha.dtype!=np.float32
                or alpha.shape!=scan.shape[:2] or output_alpha.shape!=alpha.shape
                or not output_alpha.flags.writeable
                or any(np.shares_memory(a,b) for a,b in
                    ((scan,output_alpha),(alpha,output),(alpha,output_alpha),(output,output_alpha)))):
            raise ValueError('NEGATIVE_ALPHA_INVALID')
    # Conservatively include resident pages for both full-frame mapping owners
    # plus float64 density/color intermediates of one bounded tile.
    pixels=scan.shape[0]*scan.shape[1]
    required=(24+(8 if alpha is not None else 0))*pixels+128*min(pixels,512*512)+65536
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:
        raise ValueError('NEGATIVE_RESOURCE_INVALID')
    if ram_max_bytes<required:raise ValueError('NEGATIVE_RAM_EXCEEDED')
    # Kernel validates epsilon even when strength is zero; reject before writes.
    if type(epsilon) not in (float,int) or not np.isfinite(epsilon) or not 0<epsilon<=1e-6:
        raise ValueError('NEGATIVE_INPUT_INVALID')
    check_active()
    for y in range(0,scan.shape[0],512):
        for x in range(0,scan.shape[1],512):
            check_active();tile=np.s_[y:y+512,x:x+512]
            if alpha is not None:
                a=alpha[tile]
                if not np.isfinite(a).all() or np.any(a<0) or np.any(a>1):
                    raise ValueError('NEGATIVE_ALPHA_INVALID')
            converted=invert_negative_tile(scan[tile],p,c,input_icc_sha=input_icc_sha,epsilon=epsilon)
            check_active()
            output[tile]=converted
            if alpha is not None:output_alpha[tile]=a
    check_active()

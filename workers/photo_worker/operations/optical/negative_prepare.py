"""Attempt-owned negative RGB/mask preparation; publishing remains separate."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from .negative_calibration import verify_negative_calibration
from .negative_frame import convert_negative_frame
from .negative_changes import write_negative_changed_mask


@dataclass(frozen=True)
class NegativeFiles:
    rgb: Path
    changed_mask: Path
    reservation: object
    changed_pixels: int
    review_required: bool = True
    clipping_fraction: float = 0.


@contextmanager
def prepare_negative_files(scan, params, calibration_raw, scratch, *, input_icc_sha,
        epsilon, ram_max_bytes, check_active):
    """Retain reservation until staging; no files survive context exit.

    Caller owns verified linear source/alpha refs, live qualification and all
    retained allocations outside source/RGB/mask. Alpha is inherited unchanged.
    """
    if not callable(check_active):raise ValueError('NEGATIVE_ACTIVITY_REQUIRED')
    p=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    raw=bytes(calibration_raw) if isinstance(calibration_raw,(bytes,bytearray)) else calibration_raw
    verify_negative_calibration(p,raw,input_icc_sha=input_icc_sha)
    if (not isinstance(scan,np.ndarray) or scan.dtype!=np.float32 or scan.ndim!=3
            or scan.shape[-1]!=3 or min(scan.shape[:2])<1
            or scan.shape[0]*scan.shape[1]>200_000_000):raise ValueError('NEGATIVE_FRAME_INVALID')
    pixels=scan.shape[0]*scan.shape[1]
    required=28*pixels+128*min(pixels,512*512)+65536
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:
        raise ValueError('NEGATIVE_RESOURCE_INVALID')
    if ram_max_bytes<required:raise ValueError('NEGATIVE_RAM_EXCEEDED')
    if type(epsilon) not in (int,float) or not np.isfinite(epsilon) or not 0<epsilon<=1e-6:
        raise ValueError('NEGATIVE_INPUT_INVALID')
    check_active()
    with scratch.reserve(16*pixels+32768) as reservation:
        rgb=reservation.path('negative-rgb','.npy');mask=reservation.path('negative-changed','.npy')
        output=changed=None
        try:
            output=np.lib.format.open_memmap(rgb,mode='w+',dtype='<f4',shape=scan.shape)
            changed=np.lib.format.open_memmap(mask,mode='w+',dtype='<f4',shape=scan.shape[:2])
            convert_negative_frame(scan,output,p,raw,input_icc_sha=input_icc_sha,epsilon=epsilon,
                ram_max_bytes=ram_max_bytes,check_active=check_active)
            count=write_negative_changed_mask(scan,output,changed,check_active=check_active,ram_max_bytes=ram_max_bytes)
            output.flush();changed.flush();reservation.validate();check_active()
            from .negative_statistics import negative_clipping_fraction
            fraction=negative_clipping_fraction(output,check_active=check_active)
            yield NegativeFiles(rgb,mask,reservation,count,clipping_fraction=fraction)
        finally:
            for mapping in (output,changed):
                if mapping is not None:mapping._mmap.close()


@contextmanager
def prepare_pinned_negative_files(scan,params,calibration_raw,scratch,*,input_icc_sha,
        runtime_lock,runtime_sha,ram_max_bytes,check_active):
    """Own and verify implementation pins around preparation and staged use.

    Qualification and source authority must be established by the installation;
    a runtime lock alone proves implementation identity, not scientific quality.
    """
    from .negative_runtime_lock import NegativeRuntimeLock,verify_installed_runtime
    if not callable(check_active):raise ValueError('NEGATIVE_ACTIVITY_REQUIRED')
    owned=NegativeRuntimeLock.model_validate_json(canonical_bytes(runtime_lock))
    verify_installed_runtime(owned,runtime_sha)
    owned_params=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    owned_calibration=bytes(calibration_raw) if isinstance(calibration_raw,(bytes,bytearray)) else calibration_raw
    check_active();verify_installed_runtime(owned,runtime_sha);check_active()
    with prepare_negative_files(scan,owned_params,owned_calibration,scratch,input_icc_sha=input_icc_sha,
            epsilon=owned.epsilon,ram_max_bytes=ram_max_bytes,check_active=check_active) as files:
        check_active();verify_installed_runtime(owned,runtime_sha);check_active()
        yield files
        check_active();verify_installed_runtime(owned,runtime_sha);check_active()

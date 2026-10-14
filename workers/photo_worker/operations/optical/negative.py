"""Bounded linear density inversion from the V29 imaging specification.

Caller supplies verified original calibration and linear scan tiles. This pure
kernel does not establish qualification, source authority or output publication.
"""
import numpy as np
from photo_worker.contracts.optical_negative import OpticalNegativeParams, NegativeCalibration
from photo_worker.contracts.base import canonical_bytes


def invert_negative_tile(scan, params, calibration, *, input_icc_sha, epsilon):
    p=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    c=NegativeCalibration.model_validate_json(canonical_bytes(calibration))
    if c.input_icc_sha!=input_icc_sha:
        raise ValueError('NEGATIVE_ICC_MISMATCH')
    if (type(epsilon) not in (float,int) or not np.isfinite(epsilon) or not 0<epsilon<=1e-6
            or not isinstance(scan,np.ndarray) or scan.dtype!=np.float32 or scan.ndim!=3
            or scan.shape[2]!=3 or min(scan.shape[:2])<1 or scan.shape[0]*scan.shape[1]>512*512
            or not np.isfinite(scan).all() or np.any(scan<0)):
        raise ValueError('NEGATIVE_INPUT_INVALID')
    if p.strength==0:return scan.copy()
    source=scan.astype(np.float64)
    density=-np.log(np.maximum(source,epsilon)/np.asarray(p.film_base_rgb,np.float64))
    transformed=density@np.asarray(c.density_matrix,np.float64).T+np.asarray(c.density_offset,np.float64)
    positive=np.clip(transformed/np.asarray(c.density_range,np.float64),0,1)*2**p.exposure_ev
    output=((1-p.strength)*source+p.strength*positive).astype(np.float32)
    if not np.isfinite(output).all():raise ValueError('NEGATIVE_OUTPUT_INVALID')
    return output


def invert_pinned_negative_tile(scan, params, calibration_raw, *, input_icc_sha, epsilon):
    """Bind the numerical kernel to original Params-pinned calibration bytes.

    Live source and trusted importer qualification must be established separately.
    """
    from .negative_calibration import verify_negative_calibration
    p=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    c=verify_negative_calibration(p,calibration_raw,input_icc_sha=input_icc_sha)
    return invert_negative_tile(scan,p,c,input_icc_sha=input_icc_sha,epsilon=epsilon)

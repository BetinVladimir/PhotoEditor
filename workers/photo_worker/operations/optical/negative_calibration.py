"""Original-byte validation; live qualification/importer authority is separate."""
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.optical_negative import OpticalNegativeParams, NegativeCalibration


def verify_negative_calibration(params, raw, *, input_icc_sha):
    p=OpticalNegativeParams.model_validate_json(canonical_bytes(params))
    if not isinstance(raw,(bytes,bytearray)) or not 2<=len(raw)<=65536:
        raise ValueError('NEGATIVE_CALIBRATION_INVALID')
    body=bytes(raw)
    if sha256(body).hexdigest()!=p.calibration.sha256:
        raise ValueError('NEGATIVE_CALIBRATION_HASH_MISMATCH')
    parsed=parse_json(body,65536)
    c=NegativeCalibration.model_validate_json(canonical_bytes(parsed))
    if canonical_bytes(c)!=body:
        raise ValueError('NEGATIVE_CALIBRATION_NONCANONICAL')
    if c.input_icc_sha!=input_icc_sha:
        raise ValueError('NEGATIVE_ICC_MISMATCH')
    return c

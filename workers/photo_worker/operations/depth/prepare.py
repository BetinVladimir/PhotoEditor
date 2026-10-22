"""Shared native cubic resize/minmax depth normalization."""
import cv2
import numpy as np


def normalize_depth(raw, width, height, *, invalid_mask=None):
    if (type(width) is not int or type(height) is not int or min(width,height) < 1
            or width*height > 200000000):
        raise ValueError("MODEL_OUTPUT_INVALID")
    # The model adapter returns a CPU ndarray. Reject a hostile shape before
    # float32 conversion, which can otherwise allocate another full result.
    if (not isinstance(raw, np.ndarray) or raw.ndim != 2 or
            not raw.size or raw.size > 200000000 or
            raw.dtype.kind != 'f'):
        raise ValueError('MODEL_OUTPUT_INVALID')
    raw = np.asarray(raw, np.float32)
    if not np.isfinite(raw).all():
        raise ValueError('MODEL_OUTPUT_INVALID')
    resized = cv2.resize(raw, (width, height), interpolation=cv2.INTER_CUBIC)
    if resized.shape != (height,width) or not np.isfinite(resized).all():
        raise ValueError("MODEL_OUTPUT_INVALID")
    if invalid_mask is not None:
        if (not isinstance(invalid_mask, np.ndarray) or invalid_mask.dtype != np.bool_
                or invalid_mask.shape != (height, width)):
            raise ValueError('DEPTH_INVALID_MASK')
        valid = ~invalid_mask
        if not np.any(valid):
            return np.zeros((height, width), np.float32), True
        samples = resized[valid]
    else:
        valid = None
        samples = resized
    lo, hi = float(np.min(samples)), float(np.max(samples))
    # Cubic interpolation can introduce float32 roundoff even for a constant
    # model field. Never amplify that numerical noise into artificial depth.
    raw_flat = float(np.max(raw))-float(np.min(raw)) <= 1e-12
    flat = raw_flat or hi-lo <= 1e-12
    depth = (np.zeros((height, width), np.float32) if flat else
             ((resized-lo)/(hi-lo)).astype(np.float32))
    if invalid_mask is not None:
        depth[invalid_mask] = 0
    if not np.isfinite(depth).all():
        raise ValueError("MODEL_OUTPUT_INVALID")
    return depth, flat

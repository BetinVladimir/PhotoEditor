"""Shared native cubic resize/minmax depth normalization."""
import cv2
import numpy as np


def normalize_depth(raw, width, height):
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
    lo, hi = float(np.min(resized)), float(np.max(resized))
    depth = (np.zeros((height, width), np.float32) if hi-lo <= 1e-12 else
             ((resized-lo)/(hi-lo)).astype(np.float32))
    if not np.isfinite(depth).all():
        raise ValueError("MODEL_OUTPUT_INVALID")
    return depth, hi-lo <= 1e-12

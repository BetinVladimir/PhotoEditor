"""V28 reflection_rgb proposal tiling; caller owns ICC, masks, and adoption."""

import numpy as np

from photo_worker.runtime.executor import OperationFailure, TaskCancelled

_MAX_PADDED_PIXELS = 64_000_000


def _starts(length: int, size: int, stride: int) -> list[int]:
    if length <= size:
        return [0]
    starts = list(range(0, length - size + 1, stride))
    if starts[-1] != length - size:
        starts.append(length - size)
    return starts


def reflection_proposal(srgb: np.ndarray, engine, *, check_active=lambda: None) -> np.ndarray:
    """Blend fixed-size model patches with separable sin² weights in sRGB."""
    if not callable(check_active):
        raise ValueError('MODEL_INPUT_INVALID')
    value = np.asarray(srgb)
    if (value.dtype != np.float32 or value.ndim != 3 or value.shape[2] != 3
            or min(value.shape[:2]) < 1 or value.shape[0] * value.shape[1] > 16_000_000
            or not np.isfinite(value).all() or np.any(value < 0) or np.any(value > 1)):
        raise ValueError('MODEL_INPUT_INVALID')
    size = getattr(engine, 'size', None)
    if type(size) is not int or not 128 <= size <= 2048:
        raise ValueError('MODEL_CONTRACT_ERROR')
    overlap = 64
    if overlap >= size / 2:
        raise ValueError('MODEL_CONTRACT_ERROR')

    def active():
        if check_active() is False:
            raise TaskCancelled('Reflection proposal inactive')

    height, width = value.shape[:2]
    pad_height, pad_width = max(0, size - height), max(0, size - width)
    if max(height, size) * max(width, size) > _MAX_PADDED_PIXELS:
        raise OperationFailure('RESOURCE_OOM')
    padded = np.pad(value, ((0, pad_height), (0, pad_width), (0, 0)), mode='reflect')
    full_height, full_width = padded.shape[:2]
    stride = size - overlap
    rows = _starts(full_height, size, stride)
    cols = _starts(full_width, size, stride)
    coordinate = (np.arange(size, dtype=np.float64) + .5) / size
    one_axis = np.sin(np.pi * coordinate) ** 2
    weights = np.outer(one_axis, one_axis)
    total = np.zeros((full_height, full_width), np.float64)
    accum = np.zeros((full_height, full_width, 3), np.float64)
    for y in rows:
        for x in cols:
            active()
            tile = np.ascontiguousarray(padded[y:y+size, x:x+size])
            proposal = np.asarray(engine.infer(tile, check_active=check_active))
            if (proposal.dtype != np.float32 or proposal.shape != tile.shape
                    or not np.isfinite(proposal).all() or np.any(proposal < 0)
                    or np.any(proposal > 1)):
                raise ValueError('MODEL_OUTPUT_INVALID')
            accum[y:y+size, x:x+size] += proposal * weights[:, :, None]
            total[y:y+size, x:x+size] += weights
    active()
    if not np.all(total > 0):
        raise ValueError('MODEL_OUTPUT_INVALID')
    result = (accum[:height, :width] / total[:height, :width, None]).astype(np.float32)
    if not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1):
        raise ValueError('MODEL_OUTPUT_INVALID')
    return result

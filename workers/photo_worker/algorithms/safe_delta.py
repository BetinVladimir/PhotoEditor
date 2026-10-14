"""Exact outside-mask composition for V28 local proposals in working linear RGB."""

import numpy as np


def compose_safe_delta(source, proposal, target, protected, strength):
    """Return RGB, changed mask and inference-generated mask for one tile.

    ICC conversion is the operation adapter's responsibility. Alpha is carried
    by the source WorkingImage without change.
    """
    source = np.asarray(source)
    proposal = np.asarray(proposal)
    target = np.asarray(target)
    protected = np.asarray(protected)
    if (source.dtype != np.float32 or proposal.dtype != np.float32
            or target.dtype != np.float32 or protected.dtype != np.float32
            or source.ndim != 3 or source.shape[2] != 3
            or proposal.shape != source.shape
            or target.shape != source.shape[:2]
            or protected.shape != source.shape[:2]
            or min(source.shape[:2]) < 1 or max(source.shape[:2]) > 1024
            or type(strength) not in (int, float) or not np.isfinite(strength)
            or not 0 <= strength <= 1
            or not all(np.isfinite(array).all() for array in
                       (source, proposal, target, protected))):
        raise ValueError('SAFE_DELTA_INPUT_INVALID')
    effective = np.clip(target, 0, 1).astype(np.float64)
    effective *= float(strength)
    effective[protected > 0] = 0
    changed = np.zeros(source.shape[:2], dtype=np.uint8)
    generated = (effective > 0).astype(np.uint8)
    result = source.copy()
    selected = effective > 0
    if np.any(selected):
        updated = source[selected].astype(np.float64) + (
            proposal[selected].astype(np.float64) - source[selected].astype(np.float64)
        ) * effective[selected, None]
        if not np.isfinite(updated).all() or np.any(np.abs(updated) > np.finfo(np.float32).max):
            raise ValueError('SAFE_DELTA_OUTPUT_INVALID')
        result[selected] = updated.astype(np.float32)
        changed[selected] = np.any(
            result[selected].view(np.uint32) != source[selected].view(np.uint32), axis=1
        ).astype(np.uint8)
    return result, changed, generated

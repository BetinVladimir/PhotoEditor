"""Numerical preservation proof, without model qualification or Apply authority."""
from dataclasses import dataclass
import numpy as np


@dataclass(frozen=True)
class CandidatePreservation:
    pixels: int
    changed_pixels: int
    outside_mask_unchanged: bool
    protected_unchanged: bool
    alpha_unchanged: bool


def verify_candidate_preservation(original, candidate, mask, protected, *,
        original_alpha=None, candidate_alpha=None, max_workspace_bytes,
        check_active):
    """Caller owns and pins full-canvas arrays for the duration of this proof.

    Workspace admission covers bounded verification tiles only, not caller RGB,
    mask or alpha owners. HDR values above one are preserved. An artistic change
    inside an allowed region is neither accepted nor rejected by this verifier.
    """
    if not callable(check_active):
        raise ValueError('SEMANTIC_CANDIDATE_VERIFIER_REQUIRED')
    check_active()
    if (not isinstance(original, np.ndarray) or original.dtype != np.float32
            or original.ndim != 3 or original.shape[2] != 3
            or not 1 <= original.shape[0]*original.shape[1] <= 200000000
            or not isinstance(candidate, np.ndarray) or candidate.dtype != np.float32
            or candidate.shape != original.shape):
        raise ValueError('SEMANTIC_CANDIDATE_ARRAYS_INVALID')
    shape = original.shape[:2]
    if any(not isinstance(value, np.ndarray) or value.dtype not in (np.dtype(np.float32), np.dtype(np.bool_))
           or value.shape != shape for value in (mask, protected)):
        raise ValueError('SEMANTIC_CANDIDATE_ARRAYS_INVALID')
    if (original_alpha is None) != (candidate_alpha is None):
        raise ValueError('SEMANTIC_CANDIDATE_ALPHA_CHANGED')
    if original_alpha is not None and any(not isinstance(value, np.ndarray)
            or value.dtype != np.float32 or value.shape != shape
            for value in (original_alpha, candidate_alpha)):
        raise ValueError('SEMANTIC_CANDIDATE_ALPHA_CHANGED')
    if type(max_workspace_bytes) is not int or not 8*1048576 <= max_workspace_bytes <= 2**53-1:
        raise ValueError('SEMANTIC_CANDIDATE_WORKSPACE_EXCEEDED')
    changed_count = 0
    for y in range(0, shape[0], 256):
        for x in range(0, shape[1], 256):
            check_active()
            tile = np.s_[y:y+256, x:x+256]
            before, after, weights, protection = original[tile], candidate[tile], mask[tile], protected[tile]
            for value in (before, after):
                if not np.isfinite(value).all() or np.any(value < 0):
                    raise ValueError('SEMANTIC_CANDIDATE_ARRAYS_INVALID')
            for value in (weights, protection):
                if not np.isfinite(value).all() or np.any((value < 0) | (value > 1)):
                    raise ValueError('SEMANTIC_CANDIDATE_ARRAYS_INVALID')
            changed = np.any(before.view(np.uint32) != after.view(np.uint32), axis=2)
            if np.any(changed & ((weights == 0) | (protection > 0))):
                raise ValueError('SEMANTIC_CANDIDATE_PRESERVATION_FAILED')
            changed_count += int(np.count_nonzero(changed))
            if original_alpha is not None:
                a, b = original_alpha[tile], candidate_alpha[tile]
                if (not np.isfinite(a).all() or not np.isfinite(b).all()
                        or np.any((a < 0) | (a > 1)) or np.any((b < 0) | (b > 1))
                        or not np.array_equal(a.view(np.uint32), b.view(np.uint32))):
                    raise ValueError('SEMANTIC_CANDIDATE_ALPHA_CHANGED')
    check_active()
    return CandidatePreservation(shape[0]*shape[1], changed_count, True, True, True)

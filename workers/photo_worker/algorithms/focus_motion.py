"""Reference-relative motion evidence and explicit V29 focus policy."""
from math import isfinite
import numpy as np
from .multi_motion import dilate_motion_disk3


def detect_focus_motion(frames, alphas, coverages, reference, motion, *,
                        motion_threshold, tile=256, check_active=lambda: None):
    if (not isinstance(frames, (list, tuple)) or not 2 <= len(frames) <= 30
            or not isinstance(alphas, (list, tuple)) or not isinstance(coverages, (list, tuple))
            or len(alphas) != len(frames) or len(coverages) != len(frames)
            or type(reference) is not int or not 0 <= reference < len(frames)
            or not isinstance(motion, np.ndarray) or motion.dtype != np.bool_ or motion.ndim != 2
            or not motion.flags.writeable or min(motion.shape) < 1 or motion.size > 200000000
            or type(tile) is not int or not 1 <= tile <= 512
            or type(motion_threshold) not in (int, float) or not isfinite(motion_threshold) or motion_threshold < 0):
        raise ValueError('FOCUS_MOTION_INVALID')
    for rgb, alpha, coverage in zip(frames, alphas, coverages):
        if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.shape != (*motion.shape, 3)
                or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != motion.shape
                or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_ or coverage.shape != motion.shape
                or any(np.shares_memory(motion, value) for value in (rgb, alpha, coverage))):
            raise ValueError('FOCUS_MOTION_INVALID')
    height, width = motion.shape
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); region = np.s_[y:y+tile, x:x+tile]
            ref = frames[reference][region].astype(np.float64)
            ref_visible = coverages[reference][region] & (alphas[reference][region] > 0)
            changed = np.zeros(ref_visible.shape, bool)
            for i, (rgb, alpha, coverage) in enumerate(zip(frames, alphas, coverages)):
                check_active(); a = alpha[region]; color = rgb[region].astype(np.float64)
                if not np.isfinite(color).all() or not np.isfinite(a).all() or np.any(a < 0) or np.any(a > 1):
                    raise ValueError('FOCUS_MOTION_INVALID')
                if i != reference:
                    overlap = ref_visible & coverage[region] & (a > 0)
                    changed |= overlap & (np.max(np.abs(color-ref), axis=-1) > motion_threshold)
            motion[region] = changed
    return dilate_motion_disk3(motion, check_active=check_active)


def apply_focus_motion_policy(rgb, alpha, reference_rgb, reference_alpha, motion,
                              *, policy, tile=256, check_active=lambda: None):
    """In-place review fallback; reject raises before any output modification.

    Caller discards scratch outputs on any error/cancellation. Reference data
    is copied exactly in the dilated mask, including its original opacity.
    """
    if (policy not in ('reject', 'review') or type(tile) is not int or not 1 <= tile <= 512
            or not isinstance(rgb, np.ndarray) or rgb.dtype not in (np.dtype('float32'), np.dtype('float64'))
            or rgb.ndim != 3 or rgb.shape[2] != 3 or not rgb.flags.writeable
            or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]
            or not alpha.flags.writeable or min(alpha.shape) < 1 or alpha.size > 200000000
            or not isinstance(reference_rgb, np.ndarray) or reference_rgb.dtype != np.float32 or reference_rgb.shape != rgb.shape
            or not isinstance(reference_alpha, np.ndarray) or reference_alpha.dtype != np.float32 or reference_alpha.shape != alpha.shape
            or not isinstance(motion, np.ndarray) or motion.dtype != np.bool_ or motion.shape != alpha.shape
            or np.shares_memory(rgb, alpha)
            or any(np.shares_memory(out, source) for out in (rgb, alpha) for source in (reference_rgb, reference_alpha, motion))):
        raise ValueError('FOCUS_MOTION_INVALID')
    height, width = alpha.shape; count = 0
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); region = np.s_[y:y+tile, x:x+tile]
            for value in (rgb[region], reference_rgb[region]):
                if not np.isfinite(value).all(): raise ValueError('FOCUS_MOTION_INVALID')
            for value in (alpha[region], reference_alpha[region]):
                if not np.isfinite(value).all() or np.any(value < 0) or np.any(value > 1):
                    raise ValueError('FOCUS_MOTION_INVALID')
            count += int(np.count_nonzero(motion[region]))
    if policy == 'reject' and count:
        raise ValueError('MOTION_DETECTED')
    if policy == 'review' and count:
        for y in range(0, height, tile):
            for x in range(0, width, tile):
                check_active(); region = np.s_[y:y+tile, x:x+tile]; mask = motion[region]
                rgb[region][mask] = reference_rgb[region][mask]
                alpha[region][mask] = reference_alpha[region][mask]
    check_active()
    return count/(height*width)

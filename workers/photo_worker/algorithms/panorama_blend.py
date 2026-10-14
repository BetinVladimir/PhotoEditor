"""Ordinal, tiled linear-HDR panorama feathering and overlap motion evidence."""
from math import isfinite
import cv2
import numpy as np
from .multi_motion import dilate_motion_disk3


def _block(value, x, y, width, height, halo):
    """Constant-zero halo, including beyond the full canvas border."""
    result = np.zeros((height+2*halo, width+2*halo), dtype=value.dtype)
    sh, sw = value.shape
    left, top = max(0, x-halo), max(0, y-halo)
    right, bottom = min(sw, x+width+halo), min(sh, y+height+halo)
    ox, oy = left-x+halo, top-y+halo
    result[oy:oy+bottom-top, ox:ox+right-left] = value[top:bottom, left:right]
    return result


def blend_panorama(frames, alphas, coverages, output_rgb, output_alpha, motion,
                   *, motion_threshold, tile=256, check_active=lambda: None):
    """Write to caller-admitted buffers; originals and warped inputs unchanged.

    Input ordinal fixes floating-point fold order. Coverage weights use precise
    Euclidean distance to invalid pixel centres, capped at 64 pixels. Straight
    colors are alpha-premultiplied for accumulation; no exposure compensation
    or HDR clipping occurs. Motion is pairwise RGB range on visible overlaps,
    dilated by the exact integer disk of radius 3. Returned fraction uses the
    complete output canvas as denominator; the report adapter always reviews.
    """
    if (not isinstance(frames, (list, tuple)) or not 2 <= len(frames) <= 20
            or not isinstance(alphas, (list, tuple)) or not isinstance(coverages, (list, tuple))
            or len(alphas) != len(frames) or len(coverages) != len(frames)
            or not isinstance(output_rgb, np.ndarray) or output_rgb.dtype != np.float32
            or output_rgb.ndim != 3 or output_rgb.shape[2] != 3
            or not isinstance(output_alpha, np.ndarray) or output_alpha.dtype != np.float32
            or output_alpha.shape != output_rgb.shape[:2]
            or not isinstance(motion, np.ndarray) or motion.dtype != np.bool_ or motion.shape != output_alpha.shape
            or min(output_alpha.shape) < 1 or output_alpha.size > 200000000
            or type(tile) is not int or not 1 <= tile <= 512
            or type(motion_threshold) not in (int, float) or not isfinite(motion_threshold) or motion_threshold < 0):
        raise ValueError('PANORAMA_BLEND_INVALID')
    outputs = (output_rgb, output_alpha, motion)
    if any(not out.flags.writeable for out in outputs):
        raise ValueError('PANORAMA_BLEND_INVALID')
    for i, out in enumerate(outputs):
        if any(np.shares_memory(out, other) for other in outputs[i+1:]):
            raise ValueError('PANORAMA_BLEND_ALIAS')
    for rgb, alpha, coverage in zip(frames, alphas, coverages):
        if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.shape != output_rgb.shape
                or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != output_alpha.shape
                or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_ or coverage.shape != motion.shape):
            raise ValueError('PANORAMA_BLEND_INVALID')
        if any(np.shares_memory(out, source) for out in outputs for source in (rgb, alpha, coverage)):
            raise ValueError('PANORAMA_BLEND_ALIAS')
    height, width = output_alpha.shape
    # Reject malformed originals before writing any result pixels.
    for rgb, alpha in zip(frames, alphas):
        for y in range(0, height, tile):
            for x in range(0, width, tile):
                check_active(); a = alpha[y:y+tile, x:x+tile]
                if (not np.isfinite(rgb[y:y+tile, x:x+tile]).all() or not np.isfinite(a).all()
                        or np.any(a < 0) or np.any(a > 1)):
                    raise ValueError('PANORAMA_BLEND_INVALID')
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); h, w = min(tile, height-y), min(tile, width-x)
            numerator = np.zeros((h, w, 3), np.float64)
            weight_sum = np.zeros((h, w), np.float64); alpha_sum = np.zeros((h, w), np.float64)
            low = np.full((h, w, 3), np.inf); high = np.full((h, w, 3), -np.inf)
            count = np.zeros((h, w), np.uint8)
            for rgb, alpha, coverage in zip(frames, alphas, coverages):
                check_active()
                halo = (_block(coverage, x, y, w, h, 64) & (_block(alpha, x, y, w, h, 64) > 0)).astype(np.uint8)
                distance = cv2.distanceTransform(halo, cv2.DIST_L2, cv2.DIST_MASK_PRECISE)[64:64+h, 64:64+w]
                a = alpha[y:y+h, x:x+w].astype(np.float64)
                c = rgb[y:y+h, x:x+w].astype(np.float64)
                visible = coverage[y:y+h, x:x+w] & (a > 0)
                weight = np.minimum(distance, 64).astype(np.float64)/64
                weight *= visible
                weight_sum += weight; alpha_sum += weight*a
                numerator += c*(weight*a)[..., None]
                low = np.minimum(low, np.where(visible[..., None], c, np.inf))
                high = np.maximum(high, np.where(visible[..., None], c, -np.inf)); count += visible
            color = np.zeros_like(numerator); opacity = np.zeros_like(alpha_sum)
            np.divide(numerator, alpha_sum[..., None], out=color, where=alpha_sum[..., None] > 0)
            np.divide(alpha_sum, weight_sum, out=opacity, where=weight_sum > 0)
            output_rgb[y:y+h, x:x+w] = color
            output_alpha[y:y+h, x:x+w] = opacity
            # Avoid inf-inf in empty locations; no motion without two sources.
            overlap = count >= 2
            delta = np.zeros_like(low)
            np.subtract(high, low, out=delta, where=overlap[..., None])
            motion[y:y+h, x:x+w] = overlap & (np.max(delta, axis=2) > motion_threshold)
    return dilate_motion_disk3(motion, check_active=check_active)

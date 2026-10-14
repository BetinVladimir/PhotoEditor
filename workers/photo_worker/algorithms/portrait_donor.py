"""Deterministic V28 donor geometry; caller owns identity/consent/ROI proof."""
from dataclasses import dataclass
import math
import numpy as np

_ANCHORS = np.array([*range(27, 36), 36, 39, 42, 45])


@dataclass(frozen=True)
class DonorAlignment:
    matrix: np.ndarray  # donor pixel coordinates -> source pixel coordinates
    scale: float
    rotation_degrees: float
    median_residual: float
    max_residual: float


def align_local_patch(source68, donor68, source_face_diagonal: float) -> DonorAlignment:
    source = np.asarray(source68, dtype=np.float64)
    donor = np.asarray(donor68, dtype=np.float64)
    if (source.shape != (68, 2) or donor.shape != (68, 2) or
            not np.isfinite(source).all() or not np.isfinite(donor).all() or
            not math.isfinite(source_face_diagonal) or source_face_diagonal <= 0):
        raise ValueError('DONOR_GEOMETRY')
    target = source[_ANCHORS]
    moving = donor[_ANCHORS]
    source_mean, donor_mean = target.mean(axis=0), moving.mean(axis=0)
    target_centered, donor_centered = target - source_mean, moving - donor_mean
    variance = float(np.mean(np.sum(donor_centered ** 2, axis=1)))
    if not math.isfinite(variance) or variance <= 1e-8:
        raise ValueError('DONOR_GEOMETRY')
    covariance = target_centered.T @ donor_centered / len(_ANCHORS)
    if not np.isfinite(covariance).all():
        raise ValueError('DONOR_GEOMETRY')
    u, singular, vt = np.linalg.svd(covariance)
    signs = np.array([1., -1. if np.linalg.det(u @ vt) < 0 else 1.])
    rotation = u @ np.diag(signs) @ vt
    scale = float(singular @ signs / variance)
    angle = math.degrees(math.atan2(rotation[1, 0], rotation[0, 0]))
    translation = source_mean - scale * rotation @ donor_mean
    predicted = moving @ (scale * rotation).T + translation
    residual = np.linalg.norm(predicted - target, axis=1)
    median, maximum = float(np.median(residual)), float(np.max(residual))
    if (not all(math.isfinite(x) for x in (scale, angle, median, maximum)) or
            not .8 <= scale <= 1.25 or abs(angle) > 15 or
            median > .02 * source_face_diagonal or maximum > .05 * source_face_diagonal):
        raise ValueError('DONOR_ALIGNMENT')
    matrix = np.column_stack((scale * rotation, translation))
    matrix.setflags(write=False)
    return DonorAlignment(matrix, scale, angle, median, maximum)


def sample_donor_pixels(donor_rgb, donor_to_source, source_xy, *, encoding='linear',
                        donor_alpha=None, return_alpha=False):
    """Bilinear sampling at explicitly requested source pixel centers.

    The caller can request bounded tiles/patches; no full canvas warp is allocated.
    Every requested point must have donor coverage. Coordinates are x,y pixels.
    Gamma24 corners are decoded before interpolation, without HDR clipping or
    allocating a decoded copy of the full donor canvas. Output is linear RGB.
    Optional float32 straight alpha is premultiplied before interpolation;
    return_alpha requests the sampled opacity alongside straight RGB.
    """
    donor = np.asarray(donor_rgb)
    transform = np.asarray(donor_to_source, dtype=np.float64)
    points = np.asarray(source_xy, dtype=np.float64)
    if (type(return_alpha) is not bool or encoding not in ('linear', 'gamma24') or
            donor.ndim != 3 or donor.shape[2] != 3 or donor.dtype != np.float32 or
            min(donor.shape[:2]) < 1 or transform.shape != (2, 3) or
            not np.isfinite(transform).all() or points.ndim != 2 or points.shape[1] != 2 or
            len(points) > 1_048_576 or not np.isfinite(points).all()):
        raise ValueError('DONOR_GEOMETRY')
    linear = transform[:, :2]
    determinant = float(np.linalg.det(linear))
    if not math.isfinite(determinant) or determinant <= 1e-12:
        raise ValueError('DONOR_GEOMETRY')
    inverse = np.linalg.inv(linear)
    coordinates = (points - transform[:, 2]) @ inverse.T
    height, width = donor.shape[:2]
    if (not np.isfinite(coordinates).all() or np.any(coordinates < 0) or
            np.any(coordinates[:, 0] > width - 1) or np.any(coordinates[:, 1] > height - 1)):
        raise ValueError('DONOR_COVERAGE')
    x, y = coordinates[:, 0], coordinates[:, 1]
    x0, y0 = np.floor(x).astype(np.int64), np.floor(y).astype(np.int64)
    x1, y1 = np.minimum(x0 + 1, width - 1), np.minimum(y0 + 1, height - 1)
    corners = np.stack((donor[y0, x0], donor[y0, x1], donor[y1, x0], donor[y1, x1])).astype(np.float64)
    if not np.isfinite(corners).all() or np.any(corners < 0):
        raise ValueError('DONOR_PIXELS')
    if encoding == 'gamma24':
        corners = np.power(corners, 2.4)
    wx, wy = (x - x0)[:, None], (y - y0)[:, None]
    opacity = np.ones((len(points),1),dtype=np.float64)
    if donor_alpha is not None:
        alpha = np.asarray(donor_alpha)
        if alpha.shape != donor.shape[:2] or alpha.dtype != np.float32:
            raise ValueError('DONOR_ALPHA')
        a = np.stack((alpha[y0,x0],alpha[y0,x1],alpha[y1,x0],alpha[y1,x1])).astype(np.float64)[...,None]
        if not np.isfinite(a).all() or np.any(a<0) or np.any(a>1):
            raise ValueError('DONOR_ALPHA')
        corners = corners * a
        opacity = ((a[0]*(1-wx)+a[1]*wx)*(1-wy)+(a[2]*(1-wx)+a[3]*wx)*wy)
    sampled = ((corners[0] * (1 - wx) + corners[1] * wx) * (1 - wy) +
               (corners[2] * (1 - wx) + corners[3] * wx) * wy)
    if donor_alpha is not None:
        sampled = np.divide(sampled,opacity,out=np.zeros_like(sampled),where=opacity>1e-6)
    if np.any(sampled > np.finfo(np.float32).max):
        raise ValueError('DONOR_PIXELS')
    result = sampled.astype(np.float32)
    if not np.isfinite(result).all():
        raise ValueError('DONOR_PIXELS')
    return (result,opacity[:,0].astype(np.float32)) if return_alpha else result


@dataclass(frozen=True)
class DonorColorMatch:
    gains: np.ndarray
    sample_count: int
    warning: str | None


def match_patch_color(source_ring, donor_ring) -> DonorColorMatch:
    """Match already coverage-verified linear RGB ring samples.

    Ring selection must exclude target/protected pixels and use the documented
    Gaussian mask threshold. This primitive never substitutes another ring.
    """
    source = np.asarray(source_ring)
    donor = np.asarray(donor_ring)
    if (source.ndim != 2 or source.shape[1] != 3 or source.shape != donor.shape or
            source.dtype != np.float32 or donor.dtype != np.float32 or
            len(source) > 1_048_576 or not np.isfinite(source).all() or
            not np.isfinite(donor).all() or np.any(source < 0) or np.any(donor < 0)):
        raise ValueError('DONOR_COLOR')
    count = len(source)
    if count < 64:
        gains = np.ones(3, dtype=np.float64)
        warning = 'COLOR_MATCH_UNAVAILABLE'
    else:
        # Float64 medians avoid overflow when averaging two HDR float32 values.
        source_median = np.median(source.astype(np.float64), axis=0)
        donor_median = np.median(donor.astype(np.float64), axis=0)
        gains = np.clip((source_median + 1e-8) / (donor_median + 1e-8), .8, 1.25)
        warning = None
    gains.setflags(write=False)
    return DonorColorMatch(gains, count, warning)


@dataclass(frozen=True)
class DonorMaskGeometry:
    color_ring: np.ndarray
    feather: np.ndarray


def donor_mask_geometry(mask, protected, source_face_diagonal: float) -> DonorMaskGeometry:
    """Mask geometry on an admitted patch, including the required filter halo.

    Tiled callers must provide enough halo and crop outputs to their core; this
    helper does not make truncated patch boundaries equivalent to the canvas.
    """
    import cv2
    target = np.asarray(mask)
    guard = np.asarray(protected)
    if (target.ndim != 2 or target.shape != guard.shape or min(target.shape) < 1 or
            target.dtype != np.float32 or guard.dtype != np.float32 or
            not math.isfinite(source_face_diagonal) or source_face_diagonal <= 0 or
            not np.isfinite(target).all() or not np.isfinite(guard).all() or
            np.any(target < 0) or np.any(target > 1) or np.any(guard < 0) or np.any(guard > 1)):
        raise ValueError('DONOR_MASK')
    blurred_ring = cv2.GaussianBlur(target, (0, 0), .01 * source_face_diagonal,
                                    borderType=cv2.BORDER_REFLECT_101)
    ring = (blurred_ring > .01) & (target == 0) & (guard == 0)
    feather = target * cv2.GaussianBlur(target, (0, 0), max(1., .003 * source_face_diagonal),
                                      borderType=cv2.BORDER_REFLECT_101)
    # Outside-mask values remain exactly zero even at Gaussian boundaries.
    feather[target == 0] = 0
    ring.setflags(write=False)
    feather.setflags(write=False)
    return DonorMaskGeometry(ring, feather)


def compose_donor_patch(source, sampled_donor, feather, protected, strength, gains):
    """Blend a verified linear RGB donor patch, preserving disabled pixels.

    Review is always required by the operation; this primitive cannot approve
    identity, consent, mask geometry, color warnings or a result.
    """
    from photo_worker.algorithms.portrait_local import compose_local
    base = np.asarray(source)
    donor = np.asarray(sampled_donor)
    color = np.asarray(gains, dtype=np.float64)
    if (base.dtype != np.float32 or donor.dtype != np.float32 or base.shape != donor.shape or
            base.ndim != 3 or base.shape[2] != 3 or color.shape != (3,) or
            not np.isfinite(color).all() or np.any(color < .8) or np.any(color > 1.25) or
            not np.isfinite(base).all() or not np.isfinite(donor).all() or
            np.any(base < 0) or np.any(donor < 0)):
        raise ValueError('DONOR_PIXELS')
    proposal = donor.astype(np.float64) * color
    with np.errstate(over='ignore', invalid='ignore'):
        # V2 protection is strict: even a soft positive guard forbids changes.
        guard = np.asarray(protected)
        if (guard.shape != base.shape[:2] or not np.isfinite(guard).all() or
                np.any(guard < 0) or np.any(guard > 1)):
            raise ValueError('DONOR_MASK')
        strict_guard = (guard > 0).astype(np.float32)
        result = compose_local(base, proposal, feather, strict_guard, strength)
    if not np.isfinite(result).all():
        raise ValueError('DONOR_PIXELS')
    return result

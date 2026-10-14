"""V28 linear-light hair/clothing proposals; caller owns color/lineage authority.

These functions preserve HDR range. They do not identify hair, clothing or logos,
and they do not imply worker admission or approval of a generated result.
"""
import math

import cv2
import numpy as np

_LUMA = np.array([.2126, .7152, .0722], dtype=np.float64)


def _linear(rgb):
    value = np.asarray(rgb)
    if value.ndim != 3 or value.shape[2] != 3 or min(value.shape[:2]) < 1 or not np.isfinite(value).all():
        raise ValueError('INPUT_INVALID')
    value = value.astype(np.float64)
    y = value @ _LUMA
    if np.any(y < 0):
        raise ValueError('INVALID_LUMINANCE')
    return value, y


def _unit(value):
    if not math.isfinite(value) or not 0 <= value <= 1:
        raise ValueError('INPUT_INVALID')


def rotate_chroma(rgb, hue_delta_degrees, saturation_factor):
    source, y = _linear(rgb)
    if not math.isfinite(hue_delta_degrees) or not math.isfinite(saturation_factor) or saturation_factor < 0:
        raise ValueError('INPUT_INVALID')
    theta = math.radians(hue_delta_degrees)
    u, v = source[..., 0] - y, source[..., 2] - y
    up = saturation_factor * (math.cos(theta) * u - math.sin(theta) * v)
    vp = saturation_factor * (math.sin(theta) * u + math.cos(theta) * v)
    # Scale all chroma channels together to retain luminance at the gamut edge.
    chroma = np.stack((up, (-.2126 * up - .0722 * vp) / .7152, vp), axis=-1)
    proposed = y[..., None] + chroma
    ratios = np.ones_like(proposed)
    np.divide(y[..., None], -chroma, out=ratios, where=proposed < 0)
    k = np.minimum(1, ratios.min(axis=-1))
    result = y[..., None] + k[..., None] * chroma
    # Roundoff at a mathematically zero channel must not create negatives.
    return np.maximum(result, 0).astype(np.float32)


def hair_proposal(rgb, hue_delta_degrees, saturation_factor, detail):
    _unit(detail)
    _, y = _linear(rgb)
    proposal = rotate_chroma(rgb, hue_delta_degrees, saturation_factor).astype(np.float64)
    d = detail * .25 * (y - cv2.GaussianBlur(y, (0, 0), 1.2))
    d = np.maximum(d, -proposal.min(axis=-1))
    return (proposal + d[..., None]).astype(np.float32)


def clothing_proposal(rgb, hue_delta_degrees, saturation_factor, wrinkle_strength, *, coarse_sigma=None):
    _unit(wrinkle_strength)
    _, y = _linear(rgb)
    log_y = np.log(np.maximum(y, 1e-8))
    sigma = .01 * min(y.shape) if coarse_sigma is None else coarse_sigma
    if not math.isfinite(sigma) or sigma <= 0: raise ValueError("INPUT_INVALID")
    coarse = cv2.GaussianBlur(log_y, (0, 0), sigma)
    fine = log_y - cv2.GaussianBlur(log_y, (0, 0), 1.2)
    delta = np.clip(wrinkle_strength * .5 * (coarse + fine - log_y), -math.log(1.25), math.log(1.25))
    return (rotate_chroma(rgb, hue_delta_degrees, saturation_factor) * np.exp(delta)[..., None]).astype(np.float32)


def compose_local(source, proposal, mask, protected, strength):
    _unit(strength)
    base, _ = _linear(source)
    candidate, _ = _linear(proposal)
    if candidate.shape != base.shape:
        raise ValueError('INPUT_INVALID')
    target, guard = np.asarray(mask), np.asarray(protected)
    if any(m.shape != base.shape[:2] or not np.isfinite(m).all() or np.any((m < 0) | (m > 1)) for m in (target, guard)):
        raise ValueError('INPUT_INVALID')
    weight = strength * target.astype(np.float64) * (1 - guard.astype(np.float64))
    result = (base + weight[..., None] * (candidate - base)).astype(np.float32)
    # Preserve supplied float32 pixels bit-for-bit wherever editing is disabled.
    result[weight == 0] = np.asarray(source, dtype=np.float32)[weight == 0]
    return result

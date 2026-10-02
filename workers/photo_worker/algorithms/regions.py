"""Versioned, tile-local regional color; callers validate mask provenance."""
from dataclasses import dataclass
from collections.abc import Callable, Sequence
import numpy as np
from .color import _rgb, to_linear, to_encoded, LUMA, ColorDiagnostics
from photo_worker.contracts.style import RegionAdjustment

REGION_VERSION = 'linear_rec2020_regions_v1'

@dataclass(frozen=True)
class RegionResult:
    rgb: np.ndarray
    skipped: tuple[str, ...]


def apply_regions(rgb: np.ndarray, regions: Sequence[tuple[RegionAdjustment, np.ndarray | None]],
                  diagnostic: ColorDiagnostics | None = None,
                  check_active: Callable[[], None] = lambda: None) -> RegionResult:
    """Compose same-priority deltas; higher priorities consume soft coverage first.

    Masks are borrowed per-tile float probabilities. Missing masks are explicit
    skips, never synthetic masks. Hue rotates linear Cb/Cr, preserving Rec2020 Y.
    """
    check_active()
    rgb = _rgb(rgb)
    if rgb.ndim != 3 or len(regions) > 128:
        raise ValueError('Expected RGB tile and at most128 regions')
    if len({r.region_id for r, _ in regions}) != len(regions):
        raise ValueError('Duplicate region id')
    shape = rgb.shape[:2]
    remaining = np.ones(shape, np.float32)
    ev = np.zeros(shape, np.float32)
    sat = np.zeros(shape, np.float32)
    hue = np.zeros(shape, np.float32)
    skipped = []
    ordered = sorted(regions, key=lambda item: (-item[0].priority, item[0].region_id))
    priorities = sorted({r.priority for r, _ in ordered}, reverse=True)
    for priority in priorities:
        weight = np.zeros(shape, np.float32)
        de = np.zeros(shape, np.float32)
        ds = np.zeros(shape, np.float32)
        dh = np.zeros(shape, np.float32)
        for rule, mask in ordered:
            if rule.priority != priority:
                continue
            check_active()
            if mask is None:
                skipped.append(rule.region_id)
                continue
            mask = np.asarray(mask)
            if (mask.shape != shape or mask.dtype.kind != 'f' or not np.isfinite(mask).all()
                or np.any(mask < 0) or np.any(mask > 1)):
                raise ValueError('Region mask must be matching finite probability array')
            w = mask.astype(np.float32, copy=False) * np.float32(rule.strength)
            weight += w
            de += w * np.float32(rule.ev)
            ds += w * np.float32(rule.saturation - 1)
            dh += w * np.float32(rule.hue_deg)
        scale = remaining / np.maximum(1, weight)
        ev += de * scale
        sat += ds * scale
        hue += dh * scale
        remaining *= 1 - np.minimum(1, weight)
    check_active()
    active = (ev != 0) | (sat != 0) | (hue != 0)
    if not np.any(active):
        return RegionResult(rgb.copy(), tuple(skipped))
    linear = to_linear(rgb) * np.exp2(ev)[..., None]
    y = np.sum(linear * LUMA.astype(np.float32), axis=-1)
    cb = (linear[..., 2] - y) / np.float32(2 * (1 - LUMA[2]))
    cr = (linear[..., 0] - y) / np.float32(2 * (1 - LUMA[0]))
    angle = np.deg2rad(hue)
    cosine, sine = np.cos(angle), np.sin(angle)
    cb, cr = (cb*cosine - cr*sine)*(1+sat), (cb*sine + cr*cosine)*(1+sat)
    red = y + np.float32(2*(1-LUMA[0]))*cr
    blue = y + np.float32(2*(1-LUMA[2]))*cb
    green = (y - np.float32(LUMA[0])*red - np.float32(LUMA[2])*blue)/np.float32(LUMA[1])
    output = np.stack((red, green, blue), axis=-1)
    if diagnostic is not None:
        diagnostic.record('regions', output)
    encoded = to_encoded(output)
    encoded[~active] = rgb[~active]  # exact preservation outside all corrections
    check_active()
    return RegionResult(encoded, tuple(skipped))

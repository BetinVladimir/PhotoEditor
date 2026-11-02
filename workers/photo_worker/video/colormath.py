"""Colour maths for video grading (FN-L-01…04, FN-L-09, FN-L-10)."""

import numpy as np
from scipy.interpolate import PchipInterpolator

_XN, _YN, _ZN = 0.95047, 1.0, 1.08883
_M = np.array([[0.4124564, 0.3575761, 0.1804375],
               [0.2126729, 0.7151522, 0.0721750],
               [0.0193339, 0.1191920, 0.9503041]])


def srgb_to_linear(c):
    c = np.asarray(c, dtype=np.float64)
    return np.where(c <= 0.04045, c / 12.92, ((c + 0.055) / 1.055) ** 2.4)


def linear_to_srgb(c):
    c = np.clip(np.asarray(c, dtype=np.float64), 0.0, 1.0)
    return np.where(c <= 0.0031308, 12.92 * c, 1.055 * np.power(c, 1 / 2.4) - 0.055)


def exposure(rgb, ev: float):
    out = linear_to_srgb(srgb_to_linear(rgb) * 2.0 ** ev)
    return np.round(out, 6)


def cdl(rgb, slope, offset, power, sat: float):
    c = np.asarray(rgb, dtype=np.float64)
    v = np.maximum(c * np.asarray(slope, dtype=np.float64) + np.asarray(offset, dtype=np.float64), 0.0) ** np.asarray(power, dtype=np.float64)
    y = 0.2126 * v[0] + 0.7152 * v[1] + 0.0722 * v[2]
    return np.round(np.clip(y + sat * (v - y), 0.0, 1.0), 6)


def pchip_curve(points, nodes: int = 4096):
    xs = [float(p[0]) for p in points]
    ys = [float(p[1]) for p in points]
    if len(points) < 2 or any(b <= a for a, b in zip(xs, xs[1:])) or xs[0] < 0 or xs[-1] > 1 or min(ys) < 0 or max(ys) > 1:
        raise ValueError('CURVE_RANGE')
    grid = np.linspace(0.0, 1.0, nodes)
    # outside the point range the curve is held at the end values (monotone, no extrapolation)
    return np.clip(PchipInterpolator(xs, ys, extrapolate=False)(np.clip(grid, xs[0], xs[-1])), 0.0, 1.0)


def rgb_to_lab(rgb):
    xyz = _M @ srgb_to_linear(rgb) / np.array([_XN, _YN, _ZN])
    f = np.where(xyz > 216 / 24389, np.cbrt(xyz), (24389 / 27 * xyz + 16) / 116)
    return np.array([116 * f[1] - 16, 500 * (f[0] - f[1]), 200 * (f[1] - f[2])])


def delta_e76(a, b) -> float:
    return float(np.linalg.norm(rgb_to_lab(a) - rgb_to_lab(b)))

"""3D LUT construction, `.cube` parsing/writing and effect folding (FN-L-05…08)."""

import math
from dataclasses import dataclass
from typing import Callable

import numpy as np

from . import colormath

LUT_MIN_SIZE, LUT_MAX_SIZE = 17, 65
LUT_MAX_BYTES = 20 * 1024 * 1024
LUT_MAX_LINE = 256


@dataclass
class Lut3D:
    size: int
    table: np.ndarray  # shape (size**3, 3); node index = r + N*g + N*N*b
    domain_min: float = 0.0
    domain_max: float = 1.0


def build_lut(size: int, transform: Callable[[float, float, float], tuple[float, float, float]]) -> np.ndarray:
    """FN-L-05: float32 table, red varies fastest."""
    n = size
    out = np.empty((n ** 3, 3), dtype=np.float32)
    for b in range(n):
        for g in range(n):
            for r in range(n):
                out[r + n * g + n * n * b] = transform(r / (n - 1), g / (n - 1), b / (n - 1))
    return out


def identity_lut(size: int) -> Lut3D:
    return Lut3D(size, build_lut(size, lambda r, g, b: (r, g, b)))


def parse_cube(text: str) -> Lut3D:
    """FN-L-06; every violation raises ValueError('LUT_INVALID')."""
    def bad() -> ValueError:
        return ValueError('LUT_INVALID')

    if len(text.encode('utf-8')) > LUT_MAX_BYTES:
        raise bad()
    size = None
    dmin, dmax = 0.0, 1.0
    rows: list[tuple[float, float, float]] = []
    for raw in text.splitlines():
        if len(raw) > LUT_MAX_LINE:
            raise bad()
        line = raw.strip()
        if not line or line.startswith('#'):
            continue
        parts = line.split()
        key = parts[0]
        if key == 'TITLE':
            continue
        if key == 'LUT_1D_SIZE':
            raise bad()
        if key == 'LUT_3D_SIZE':
            if size is not None or len(parts) != 2 or not parts[1].isdigit():
                raise bad()
            size = int(parts[1])
            if not LUT_MIN_SIZE <= size <= LUT_MAX_SIZE:
                raise bad()
        elif key in ('DOMAIN_MIN', 'DOMAIN_MAX'):
            if len(parts) != 4:
                raise bad()
            try:
                vals = [float(x) for x in parts[1:]]
            except ValueError:
                raise bad() from None
            if len(set(vals)) != 1 or not all(math.isfinite(v) for v in vals):
                raise bad()
            if key == 'DOMAIN_MIN':
                dmin = vals[0]
            else:
                dmax = vals[0]
        else:
            if size is None or len(parts) != 3:
                raise bad()
            try:
                row = tuple(float(x) for x in parts)
            except ValueError:
                raise bad() from None
            rows.append(row)  # type: ignore[arg-type]
    if size is None or not dmin < dmax or len(rows) != size ** 3:
        raise bad()
    table = np.array(rows, dtype=np.float64)
    if not np.all(np.isfinite(table)) or table.min() < dmin or table.max() > dmax:
        raise bad()
    return Lut3D(size, table.astype(np.float32), dmin, dmax)


def write_cube(lut: Lut3D) -> str:
    lines = [f'LUT_3D_SIZE {lut.size}']
    lines += [f'{r:.6f} {g:.6f} {b:.6f}' for r, g, b in lut.table]
    return '\n'.join(lines) + '\n'


def _sample(lut: Lut3D, rgb: np.ndarray) -> np.ndarray:
    """Trilinear lookup of one colour in [0;1]^3."""
    n = lut.size
    pos = np.clip(rgb, 0.0, 1.0) * (n - 1)
    i0 = np.minimum(np.floor(pos).astype(int), n - 2)
    f = pos - i0
    t = lut.table.astype(np.float64)
    out = np.zeros(3)
    for dr in (0, 1):
        for dg in (0, 1):
            for db in (0, 1):
                w = (f[0] if dr else 1 - f[0]) * (f[1] if dg else 1 - f[1]) * (f[2] if db else 1 - f[2])
                out += w * t[(i0[0] + dr) + n * (i0[1] + dg) + n * n * (i0[2] + db)]
    return out


_ORDER = {'color.basic': 0, 'color.cdl': 1, 'color.curves': 2, 'color.lut3d': 3}


def _apply_effect(effect: dict, rgb: np.ndarray, luts: dict[str, Lut3D]) -> np.ndarray:
    t, p = effect['type'], effect['params']
    if t == 'color.basic':
        if set(p) - {'exposure_ev'}:
            # only exposure has normative maths in docs/video (FN-L-02); the other basic sliders are not specified yet
            raise ValueError('LUT_EFFECT_UNSPECIFIED')
        return np.asarray(colormath.exposure(rgb, p.get('exposure_ev', 0.0)))
    if t == 'color.cdl':
        return np.asarray(colormath.cdl(rgb, p['slope'], p['offset'], p['power'], p['saturation']))
    if t == 'color.curves':
        out = rgb.copy()
        grid = np.linspace(0.0, 1.0, 4096)
        for key in ('master',):
            if key in p:
                out = np.interp(out, grid, colormath.pchip_curve(p[key]))
        for ch, key in enumerate(('r', 'g', 'b')):
            if key in p:
                out[ch] = np.interp(out[ch], grid, colormath.pchip_curve(p[key]))
        return out
    if t == 'color.lut3d':
        lut = luts[p['lut']['media_id']]
        return (1 - p['intensity']) * rgb + p['intensity'] * _sample(lut, rgb)
    raise ValueError('LUT_EFFECT_UNSPECIFIED')


def fold_effects_to_lut(effects: list[dict], size: int = 65, luts: dict[str, Lut3D] | None = None) -> Lut3D:
    """FN-L-08: basic → cdl → curves → lut3d, repeats in list order; empty list gives the identity."""
    active = [e for e in effects if e.get('enabled', True) and e['type'] in _ORDER]
    ordered = sorted(enumerate(active), key=lambda ie: (_ORDER[ie[1]['type']], ie[0]))
    luts = luts or {}

    def transform(r: float, g: float, b: float) -> tuple[float, float, float]:
        rgb = np.array([r, g, b], dtype=np.float64)
        for _, e in ordered:
            rgb = _apply_effect(e, rgb, luts)
        return tuple(np.clip(rgb, 0.0, 1.0))  # type: ignore[return-value]

    return Lut3D(size, build_lut(size, transform))

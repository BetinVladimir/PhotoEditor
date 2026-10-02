from __future__ import annotations

from dataclasses import dataclass, field

import numpy as np
from numpy.typing import NDArray

from photo_worker.contracts.style import GlobalColor, LUT, LocalColor, Disabled

RGB = NDArray[np.floating]
KNOTS = np.array([0, .0625, .125, .25, .5, .75, 1], dtype=np.float64)
LUMA = np.array([.2627, .6780, .0593], dtype=np.float64)
RGB_TO_XYZ = np.array([[.6369580483,.1446169036,.1688809752],
                      [.2627002120,.6779980715,.0593017165],
                      [0,.0280726930,1.0609850577]])
BRADFORD = np.array([[1.0478112,.0228866,-.0501270],
                    [.0295424,.9904844,-.0170491],
                    [-.0092345,.0150436,.7521316]])
D50 = np.array([.96422,1,.82521])


@dataclass
class ColorDiagnostics:
    clipping: dict[str, dict[str, float]] = field(default_factory=dict)

    def record(self, stage: str, value: RGB) -> None:
        self.clipping[stage] = {"below_zero": float(np.mean(value < 0)),
                                "above_one": float(np.mean(value > 1))}


def _rgb(value: RGB) -> RGB:
    value = np.asarray(value)
    if value.ndim < 2 or value.shape[-1] != 3 or not np.isfinite(value).all():
        raise ValueError("Expected finite RGB array with last dimension 3")
    return value.astype(np.float32, copy=False)


def to_linear(rgb: RGB) -> RGB:
    return np.power(np.clip(rgb, 0, 1), 2.4).astype(np.float32)


def to_encoded(rgb: RGB) -> RGB:
    return np.power(np.clip(rgb, 0, 1), 1/2.4).astype(np.float32)


def expand_lut(lut: LUT) -> RGB:
    values = np.asarray(lut.values, dtype=np.float32)
    shape = (lut.size, lut.size, lut.size, 3)
    return np.broadcast_to(values, shape) if lut.encoding == "constant" else values.reshape(shape)


def identity_lut(size: int) -> RGB:
    if size not in (17, 33, 65):
        raise ValueError("Unsupported LUT size")
    x = np.linspace(0, 1, size, dtype=np.float32)
    return np.stack(np.meshgrid(x, x, x, indexing="ij"), axis=-1)


def trilinear(grid: RGB, points: RGB) -> RGB:
    points = _rgb(points)
    size = grid.shape[0]
    if grid.shape != (size, size, size, 3) or size < 2 or not np.isfinite(grid).all():
        raise ValueError("LUT must be finite cubic RGB grid")
    q = np.clip(points, 0, 1) * (size - 1)
    base = np.minimum(np.floor(q).astype(np.int32), size - 2)
    weights = q - base
    result = np.zeros_like(points, dtype=np.float32)
    for r in (0, 1):
        for g in (0, 1):
            for b in (0, 1):
                idx = base + np.array([r, g, b], dtype=np.int32)
                w = ((weights[...,0] if r else 1-weights[...,0]) *
                     (weights[...,1] if g else 1-weights[...,1]) *
                     (weights[...,2] if b else 1-weights[...,2]))
                result += grid[idx[...,0], idx[...,1], idx[...,2]] * w[...,None]
    return result


def _clamp(value: RGB, stage: str, diagnostic: ColorDiagnostics | None) -> RGB:
    if diagnostic is not None:
        diagnostic.record(stage, value)
    return np.clip(value, 0, 1)


def apply_global(rgb: RGB, config: GlobalColor, diagnostic: ColorDiagnostics | None = None) -> RGB:
    rgb = _rgb(rgb)
    linear = to_linear(rgb) * np.asarray(config.gains_rgb, dtype=np.float32)
    encoded = to_encoded(_clamp(linear, "gains", diagnostic))
    curved = np.interp(encoded, KNOTS, config.master_curve).astype(np.float32)
    curved = np.stack([np.interp(curved[...,c], KNOTS, config.rgb_curves[c]) for c in range(3)], axis=-1).astype(np.float32)
    linear = to_linear(curved)
    y = np.sum(linear * LUMA.astype(np.float32), axis=-1, keepdims=True)
    saturated = linear + (config.saturation-1) * (linear-y)
    encoded = to_encoded(_clamp(saturated, "saturation", diagnostic))
    output = encoded + trilinear(expand_lut(config.residual_lut), encoded)
    return _clamp(output, "residual_lut", diagnostic).astype(np.float32)


def apply_local(rgb: RGB, person: RGB | None, config: LocalColor | Disabled, diagnostic: ColorDiagnostics | None = None) -> RGB:
    rgb = _rgb(rgb)
    if not config.enabled:
        return rgb.copy()
    if person is None or person.shape != rgb.shape[:-1] or not np.isfinite(person).all():
        raise ValueError("Active local color requires a matching finite person probability mask")
    p = np.clip(person, 0, 1)[...,None]
    linear = to_linear(rgb)
    def region(ev: float, saturation: float) -> RGB:
        exposed = linear * np.float32(2**ev)
        y = np.sum(exposed * LUMA.astype(np.float32), axis=-1, keepdims=True)
        return exposed + (saturation-1) * (exposed-y)
    output = region(config.person_ev, config.person_saturation)*p + region(config.background_ev, config.background_saturation)*(1-p)
    if diagnostic is not None:
        diagnostic.record("local", output)
    return to_encoded(output)


def apply_series(rgb: RGB, ev: float, gains: list[float], diagnostic: ColorDiagnostics | None = None) -> RGB:
    """Apply one bounded SeriesMatch correction in linear light."""
    rgb = _rgb(rgb)
    gain = np.asarray(gains, dtype=np.float32)
    if gain.shape != (3,) or not np.isfinite(gain).all() or not -.35 <= ev <= .35 or np.any(gain < .95) or np.any(gain > 1.05):
        raise ValueError("Series correction outside renderer bounds")
    corrected = to_linear(rgb) * np.float32(2**ev) * gain
    return to_encoded(_clamp(corrected, "series", diagnostic))


def apply_strength(base: RGB, processed: RGB, strength: float) -> RGB:
    if not 0 <= strength <= 1 or base.shape != processed.shape:
        raise ValueError("Strength or aligned base shape invalid")
    if strength == 0:
        return _rgb(base).copy()
    if strength == 1:
        return _rgb(processed).copy()
    return to_encoded(to_linear(_rgb(base))*(1-strength)+to_linear(_rgb(processed))*strength)


def compile_absolute_lut(config: GlobalColor, size: int = 33) -> LUT:
    if size not in (33, 65):
        raise ValueError("Absolute LUT must be 33 or 65")
    values = apply_global(identity_lut(size), config)
    return LUT(kind="absolute", size=size, encoding="dense", values=values.reshape(-1).tolist())


def rgb_to_lab_d50(rgb: RGB) -> RGB:
    c = np.asarray(rgb, dtype=np.float64)
    xyz = np.power(np.clip(c, 0, 1), 2.4) @ RGB_TO_XYZ.T @ BRADFORD.T
    t = xyz/D50
    delta = 6/29
    f = np.where(t > delta**3, np.cbrt(t), t/(3*delta**2)+4/29)
    return np.stack([116*f[...,1]-16,500*(f[...,0]-f[...,1]),200*(f[...,1]-f[...,2])], axis=-1)


def render_color_tiled(source: RGB, config: GlobalColor, tile: int, target: RGB | None = None) -> RGB:
    source = _rgb(source)
    if source.ndim != 3 or tile < 1:
        raise ValueError("Expected HxWx3 source and positive tile size")
    if target is None:
        target = np.empty_like(source)
    if target.shape != source.shape or target.dtype != np.float32:
        raise ValueError("Target must be float32 matching source")
    h, w = source.shape[:2]
    for y in range(0, h, tile):
        for x in range(0, w, tile):
            target[y:y+tile,x:x+tile] = apply_global(source[y:y+tile,x:x+tile], config)
    return target

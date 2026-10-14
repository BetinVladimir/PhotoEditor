"""Tiled linear HDR sampling for retained multi-input half-pixel fields."""
from dataclasses import dataclass
import numpy as np


@dataclass(frozen=True)
class MultiSampleTile:
    x: int
    y: int
    rgb: np.ndarray
    alpha: np.ndarray
    coverage: np.ndarray


def sample_inverse_field(rgb, alpha, field, coverage, *, tile=256,
                         check_active=lambda: None):
    """Yield straight RGB/alpha via premultiplied bilinear interpolation.

    Caller verifies original WorkingImage/ICC, converts RGB into linear working
    light and normalizes legacy alpha before calling. No clipping or transfer
    conversion occurs here; transparent/uncovered pixels have exact zero RGB.
    """
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3
            or rgb.shape[2] != 3 or min(rgb.shape[:2]) < 1 or rgb.shape[0]*rgb.shape[1] > 200000000
            or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]
            or not isinstance(field, np.ndarray) or field.dtype != np.float32 or field.ndim != 3 or field.shape[2] != 2
            or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_ or coverage.shape != field.shape[:2]
            or min(coverage.shape) < 1 or coverage.size > 200000000
            or type(tile) is not int or not 1 <= tile <= 512):
        raise ValueError('MULTI_SAMPLE_INPUT_INVALID')
    sh, sw = alpha.shape
    # Validate the entire original, including pixels not reached by the warp.
    for y in range(0, sh, tile):
        for x in range(0, sw, tile):
            check_active(); a = alpha[y:y+tile, x:x+tile]
            if (not np.isfinite(rgb[y:y+tile, x:x+tile]).all() or not np.isfinite(a).all()
                    or np.any(a < 0) or np.any(a > 1)):
                raise ValueError('MULTI_SAMPLE_INPUT_INVALID')
    height, width = coverage.shape
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); coordinates = field[y:y+tile, x:x+tile]
            if not np.isfinite(coordinates).all():
                raise ValueError('MULTI_SAMPLE_FIELD_INVALID')
            u = coordinates[..., 0].astype(np.float64)-.5
            v = coordinates[..., 1].astype(np.float64)-.5
            valid = coverage[y:y+tile, x:x+tile] & (u >= 0) & (u <= sw-1) & (v >= 0) & (v <= sh-1)
            u = np.clip(u, 0, sw-1); v = np.clip(v, 0, sh-1)
            x0 = np.floor(u).astype(np.int64); y0 = np.floor(v).astype(np.int64)
            x1 = np.minimum(x0+1, sw-1); y1 = np.minimum(y0+1, sh-1)
            dx, dy = u-x0, v-y0
            opacity = np.zeros(u.shape, dtype=np.float64)
            premult = np.zeros((*u.shape, 3), dtype=np.float64)
            for sx, sy, weight in ((x0, y0, (1-dx)*(1-dy)), (x1, y0, dx*(1-dy)),
                                   (x0, y1, (1-dx)*dy), (x1, y1, dx*dy)):
                check_active(); a = alpha[sy, sx].astype(np.float64)*weight
                opacity += a; premult += rgb[sy, sx].astype(np.float64)*a[..., None]
            output = np.zeros_like(premult)
            np.divide(premult, opacity[..., None], out=output, where=opacity[..., None] > 0)
            output[~valid] = 0; opacity[~valid] = 0
            color = output.astype(np.float32); a = opacity.astype(np.float32)
            if not np.isfinite(color).all():
                raise ValueError('MULTI_SAMPLE_OUTPUT_INVALID')
            color.flags.writeable = False; a.flags.writeable = False; valid.flags.writeable = False
            check_active()
            yield MultiSampleTile(x, y, color, a, valid)

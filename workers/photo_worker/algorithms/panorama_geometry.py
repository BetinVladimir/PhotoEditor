"""Bounded V29 panorama canvas planning and retained cylindrical inverse map.

Geometry only: callers retain fields in lineage, materialize admitted scratch
arrays and perform ICC-aware sampling. No source pixels are generated here.
"""
from dataclasses import dataclass
from math import ceil, floor
import numpy as np
from .multi_registration import _matrix, _shapes


@dataclass(frozen=True)
class PanoramaCanvas:
    width: int
    height: int
    left: int
    top: int
    source_to_output: tuple[np.ndarray, ...]


def plan_panorama_canvas(matrices, source_shapes, *, max_pixels,
                         check_active=lambda: None):
    shapes = _shapes(source_shapes)
    if (len(shapes)>20 or not isinstance(matrices, (list, tuple)) or len(matrices) != len(shapes)
            or type(max_pixels) is not int or not 1 <= max_pixels <= 200000000):
        raise ValueError('PANORAMA_CANVAS_INVALID')
    minimum = np.array([np.inf, np.inf]); maximum = -minimum
    owned = []
    # Full image edge corners, converted to/from integer-centre matrices.
    plus = np.array([[1., 0, .5], [0, 1, .5], [0, 0, 1]])
    minus = np.array([[1., 0, -.5], [0, 1, -.5], [0, 0, 1]])
    for matrix, shape in zip(matrices, shapes):
        check_active(); h = _matrix(matrix, shape); owned.append(h)
        height, width = shape
        points = plus @ h @ minus @ np.array([[0, width, width, 0], [0, 0, height, height], [1, 1, 1, 1.]])
        denominator = points[2]
        if not (np.all(denominator > 1e-8) or np.all(denominator < -1e-8)):
            raise ValueError('UNSAFE_REGISTRATION')
        xy = points[:2]/denominator
        if not np.isfinite(xy).all() or np.max(np.abs(xy)) > 9007199254740991:
            raise ValueError('PANORAMA_CANVAS_INVALID')
        minimum = np.minimum(minimum, xy.min(axis=1)); maximum = np.maximum(maximum, xy.max(axis=1))
    left, top = map(floor, minimum); right, bottom = map(ceil, maximum)
    width, height = right-left, bottom-top
    if width < 1 or height < 1 or width > 2147483647 or height > 2147483647 or width*height > max_pixels:
        raise ValueError('PANORAMA_PIXEL_LIMIT')
    shift = np.array([[1., 0, -left], [0, 1, -top], [0, 0, 1.]])
    result = []
    for h in owned:
        value = shift @ h; value.flags.writeable = False; result.append(value)
    check_active()
    return PanoramaCanvas(width, height, left, top, tuple(result))


def write_cylindrical_inverse(field, coverage, *, tile_rows=64,
                              check_active=lambda: None):
    """Write original half-pixel centres into caller-admitted output buffers.

    Projection dimensions equal source dimensions, f=max(W,H), optical centre
    at (W/2,H/2). Invalid coordinates are zero with false coverage, never NaN.
    Temporary coordinate storage is bounded by tile_rows*width, not full image.
    """
    if (not isinstance(field, np.ndarray) or field.dtype != np.float32 or field.ndim != 3
            or field.shape[2] != 2 or not field.flags.writeable
            or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_
            or coverage.shape != field.shape[:2] or not coverage.flags.writeable
            or np.shares_memory(field, coverage)
            or type(tile_rows) is not int or not 1 <= tile_rows <= 256):
        raise ValueError('CYLINDRICAL_BUFFER_INVALID')
    height, width = coverage.shape
    if min(height, width) < 1 or height*width > 200000000:
        raise ValueError('CYLINDRICAL_PIXEL_LIMIT')
    focal = max(height, width); cx, cy = width/2, height/2
    u = np.arange(width, dtype=np.float64)+.5
    theta = (u-cx)/focal; cosine = np.cos(theta)
    safe = np.abs(theta) < np.pi/2
    x = focal*np.tan(theta)+cx
    for start in range(0, height, tile_rows):
        check_active(); end = min(height, start+tile_rows)
        v = np.arange(start, end, dtype=np.float64)[:, None]+.5
        y = (v-cy)/cosine[None, :]+cy
        valid = safe[None, :] & (x[None, :] >= .5) & (x[None, :] <= width-.5) & (y >= .5) & (y <= height-.5)
        field[start:end, :, 0] = np.where(valid, x[None, :], 0)
        field[start:end, :, 1] = np.where(valid, y, 0)
        coverage[start:end] = valid
    check_active()


def write_registration_inverse(matrix, source_shape, field, coverage, *, tile=256,
                               check_active=lambda: None):
    """Map output integer centres to original half-pixel source centres.

    The field is temporary sampling evidence for planar registration; retained
    lineage uses the exact source→output matrix rather than a false dense warp.
    """
    if (not isinstance(source_shape, tuple) or len(source_shape) != 2
            or any(type(v) is not int or v < 1 for v in source_shape) or source_shape[0]*source_shape[1] > 200000000
            or not isinstance(field, np.ndarray) or field.dtype != np.float32 or field.ndim != 3 or field.shape[2] != 2
            or not field.flags.writeable or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_
            or coverage.shape != field.shape[:2] or not coverage.flags.writeable
            or min(coverage.shape) < 1 or coverage.size > 200000000 or np.shares_memory(field, coverage)
            or type(tile) is not int or not 1 <= tile <= 512):
        raise ValueError('REGISTRATION_FIELD_INVALID')
    h = _matrix(matrix, source_shape); inverse = np.linalg.inv(h)
    height, width = coverage.shape; sh, sw = source_shape
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); bh, bw = min(tile, height-y), min(tile, width-x)
            xx = np.arange(x, x+bw, dtype=np.float64)[None, :]
            yy = np.arange(y, y+bh, dtype=np.float64)[:, None]
            denominator = inverse[2, 0]*xx+inverse[2, 1]*yy+inverse[2, 2]
            safe = np.abs(denominator) > 1e-10
            sx = np.zeros((bh, bw), np.float64); sy = np.zeros_like(sx)
            np.divide(inverse[0, 0]*xx+inverse[0, 1]*yy+inverse[0, 2], denominator, out=sx, where=safe)
            np.divide(inverse[1, 0]*xx+inverse[1, 1]*yy+inverse[1, 2], denominator, out=sy, where=safe)
            valid = safe & np.isfinite(sx) & np.isfinite(sy) & (sx >= 0) & (sx <= sw-1) & (sy >= 0) & (sy <= sh-1)
            field[y:y+bh, x:x+bw, 0] = np.where(valid, sx+.5, 0)
            field[y:y+bh, x:x+bw, 1] = np.where(valid, sy+.5, 0)
            coverage[y:y+bh, x:x+bw] = valid
    check_active()

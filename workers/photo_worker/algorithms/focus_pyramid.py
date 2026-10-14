"""V29 focus weights and scratch-buffer pyramid primitives.

All filters use float64 arithmetic, Gaussian sigma=1 with a 9-tap support
(radius 4), global REFLECT_101 boundaries and fixed global pixel centres.
Callers own/admit disk-backed arrays; no full-frame temporary is allocated.
"""
from math import floor, log2
import cv2
import numpy as np


def pyramid_shapes(shape):
    if (not isinstance(shape, tuple) or len(shape) != 2
            or any(type(v) is not int or v < 1 for v in shape) or shape[0]*shape[1] > 200000000):
        raise ValueError('FOCUS_SHAPE_INVALID')
    levels = max(1, min(6, floor(log2(min(shape)))-4))
    result = [shape]
    for _ in range(levels):
        result.append(tuple((v+1)//2 for v in result[-1]))
    return tuple(result)


def _indices(start, count, size):
    indices = np.arange(start, start+count, dtype=np.int64)
    if size == 1:
        return np.zeros_like(indices)
    period = 2*(size-1); indices %= period
    return np.minimum(indices, period-indices)


def _halo(value, x, y, width, height, radius):
    yy = _indices(y-radius, height+2*radius, value.shape[0])
    xx = _indices(x-radius, width+2*radius, value.shape[1])
    return value[yy[:, None], xx[None, :]].astype(np.float64)


def _buffers(source, output, tile):
    if (not isinstance(source, np.ndarray) or source.dtype not in (np.dtype('float32'), np.dtype('float64'))
            or source.ndim not in (2, 3) or source.ndim == 3 and source.shape[2] != 3
            or not isinstance(output, np.ndarray) or output.dtype not in (np.dtype('float32'), np.dtype('float64'))
            or not output.flags.writeable or np.shares_memory(source, output)
            or type(tile) is not int or not 1 <= tile <= 512):
        raise ValueError('FOCUS_BUFFER_INVALID')
    pyramid_shapes(source.shape[:2])
    pyramid_shapes(output.shape[:2])


def write_focus_weights(rgb, coverage, output, *, luma, alpha=None, tile=256, check_active=lambda: None):
    """Write (Gaussian(abs(Laplacian(Y)))+1e-6)^2 * visible coverage.

    Explicit luma coefficients belong to the admitted working ICC primaries.
    Float64 weight buffers retain HDR dynamic range without squaring overflow.
    """
    _buffers(rgb, output, tile)
    coefficients = np.asarray(luma, dtype=np.float64)
    if (rgb.ndim != 3 or output.dtype != np.float64 or output.shape != rgb.shape[:2]
            or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_ or coverage.shape != output.shape
            or np.shares_memory(coverage, output) or coefficients.shape != (3,)
            or alpha is not None and (not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32
                                      or alpha.shape != output.shape or np.shares_memory(alpha, output))
            or not np.isfinite(coefficients).all() or np.any(coefficients < 0)
            or abs(float(coefficients.sum())-1) > 1e-6):
        raise ValueError('FOCUS_WEIGHT_INVALID')
    height, width = output.shape
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); h, w = min(tile, height-y), min(tile, width-x)
            block = _halo(rgb, x, y, w, h, 5)
            if not np.isfinite(block).all():
                raise ValueError('FOCUS_INPUT_INVALID')
            signal = np.sum(block*coefficients, axis=-1)
            laplacian = np.abs(cv2.Laplacian(signal, cv2.CV_64F, ksize=3, borderType=cv2.BORDER_REFLECT_101))
            focus = cv2.GaussianBlur(laplacian, (9, 9), 1., borderType=cv2.BORDER_REFLECT_101)[5:5+h, 5:5+w]
            weight = (focus+1e-6)**2
            visible = coverage[y:y+h, x:x+w]
            if alpha is not None:
                opacity = alpha[y:y+h, x:x+w]
                if not np.isfinite(opacity).all() or np.any(opacity < 0) or np.any(opacity > 1):
                    raise ValueError('FOCUS_INPUT_INVALID')
                visible = visible & (opacity > 0)
            output[y:y+h, x:x+w] = weight*visible
    check_active()


def gaussian_downsample(source, output, *, tile=256, check_active=lambda: None):
    """Gaussian sigma 1 then even global centres, including odd dimensions."""
    _buffers(source, output, tile)
    expected = tuple((v+1)//2 for v in source.shape[:2])+source.shape[2:]
    if output.shape != expected:
        raise ValueError('FOCUS_DOWNSAMPLE_INVALID')
    height, width = output.shape[:2]
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); h, w = min(tile, height-y), min(tile, width-x)
            block = _halo(source, 2*x, 2*y, 2*w-1, 2*h-1, 4)
            if not np.isfinite(block).all():
                raise ValueError('FOCUS_INPUT_INVALID')
            filtered = cv2.GaussianBlur(block, (9, 9), 1., borderType=cv2.BORDER_REFLECT_101)
            output[y:y+h, x:x+w] = filtered[4:4+2*h-1:2, 4:4+2*w-1:2]
    check_active()


def bilinear_upsample(source, output, *, tile=256, check_active=lambda: None):
    """Resize to the exact finer shape using global half-pixel coordinates."""
    _buffers(source, output, tile)
    if (source.ndim != output.ndim or source.shape[2:] != output.shape[2:]
            or any(s != (o+1)//2 for s, o in zip(source.shape[:2], output.shape[:2]))):
        raise ValueError('FOCUS_UPSAMPLE_INVALID')
    sh, sw = source.shape[:2]; height, width = output.shape[:2]
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); h, w = min(tile, height-y), min(tile, width-x)
            u = np.clip((np.arange(x, x+w)+.5)*sw/width-.5, 0, sw-1)
            v = np.clip((np.arange(y, y+h)+.5)*sh/height-.5, 0, sh-1)
            x0 = np.floor(u).astype(np.int64); y0 = np.floor(v).astype(np.int64)
            x1 = np.minimum(x0+1, sw-1); y1 = np.minimum(y0+1, sh-1)
            dx = u-x0; dy = v-y0
            result = np.zeros((h, w)+source.shape[2:], dtype=np.float64)
            for sx, sy, weight in ((x0, y0, (1-dy[:, None])*(1-dx[None, :])),
                                   (x1, y0, (1-dy[:, None])*dx[None, :]),
                                   (x0, y1, dy[:, None]*(1-dx[None, :])),
                                   (x1, y1, dy[:, None]*dx[None, :])):
                block = source[sy[:, None], sx[None, :]].astype(np.float64)
                if not np.isfinite(block).all():
                    raise ValueError('FOCUS_INPUT_INVALID')
                result += block*(weight[..., None] if source.ndim == 3 else weight)
            output[y:y+h, x:x+w] = result
    check_active()


def _rgb_operands(inputs, output, tile):
    if not isinstance(inputs, (list, tuple)) or not inputs:
        raise ValueError('FOCUS_LEVEL_INVALID')
    for source in inputs:
        _buffers(source, output, tile)
        if source.ndim != 3 or source.shape != output.shape:
            raise ValueError('FOCUS_LEVEL_INVALID')


def write_laplacian_band(fine, expanded_coarse, output, *, tile=256, check_active=lambda: None):
    """Write G(level)-upsample(G(level+1)) into an admitted float64 band."""
    _rgb_operands([fine, expanded_coarse], output, tile)
    if output.dtype != np.float64:
        raise ValueError('FOCUS_LEVEL_INVALID')
    height, width = output.shape[:2]
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); a = fine[y:y+tile, x:x+tile].astype(np.float64)
            b = expanded_coarse[y:y+tile, x:x+tile].astype(np.float64)
            if not np.isfinite(a).all() or not np.isfinite(b).all():
                raise ValueError('FOCUS_INPUT_INVALID')
            output[y:y+tile, x:x+tile] = a-b
    check_active()


def blend_focus_level(bands, weights, reference, output, *, tile=256, check_active=lambda: None):
    """Normalized weighted bands folded by source ordinal; zero→reference band.

    Weights are the corresponding Gaussian focus-weight pyramid level. Color
    bands may be negative or HDR. Float64 accumulation buffers are required
    throughout reconstruction; only final operation output converts to float32.
    """
    if (not isinstance(bands, (list, tuple)) or not 2 <= len(bands) <= 30
            or not isinstance(weights, (list, tuple)) or len(weights) != len(bands)
            or type(reference) is not int or not 0 <= reference < len(bands)):
        raise ValueError('FOCUS_LEVEL_INVALID')
    _rgb_operands(bands, output, tile)
    if output.dtype != np.float64:
        raise ValueError('FOCUS_LEVEL_INVALID')
    for weight in weights:
        if (not isinstance(weight, np.ndarray) or weight.dtype != np.float64
                or weight.shape != output.shape[:2] or np.shares_memory(weight, output)):
            raise ValueError('FOCUS_LEVEL_INVALID')
    height, width = output.shape[:2]
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); h, w = min(tile, height-y), min(tile, width-x)
            total = np.zeros((h, w), np.float64); numerator = np.zeros((h, w, 3), np.float64)
            for band, weight in zip(bands, weights):
                check_active(); a = band[y:y+h, x:x+w].astype(np.float64)
                b = weight[y:y+h, x:x+w]
                if not np.isfinite(a).all() or not np.isfinite(b).all() or np.any(b < 0):
                    raise ValueError('FOCUS_INPUT_INVALID')
                total += b; numerator += a*b[..., None]
            result = bands[reference][y:y+h, x:x+w].astype(np.float64, copy=True)
            np.divide(numerator, total[..., None], out=result, where=total[..., None] > 0)
            if not np.isfinite(result).all() or not np.isfinite(total).all():
                raise ValueError('FOCUS_OUTPUT_INVALID')
            output[y:y+h, x:x+w] = result
    check_active()


def reconstruct_focus_level(band, expanded_coarse, output, *, tile=256, check_active=lambda: None):
    """Write blended band + expanded reconstructed coarser level."""
    _rgb_operands([band, expanded_coarse], output, tile)
    if output.dtype != np.float64:
        raise ValueError('FOCUS_LEVEL_INVALID')
    height, width = output.shape[:2]
    for y in range(0, height, tile):
        for x in range(0, width, tile):
            check_active(); a = band[y:y+tile, x:x+tile].astype(np.float64)
            b = expanded_coarse[y:y+tile, x:x+tile].astype(np.float64)
            result = a+b
            if not np.isfinite(a).all() or not np.isfinite(b).all() or not np.isfinite(result).all():
                raise ValueError('FOCUS_OUTPUT_INVALID')
            output[y:y+tile, x:x+tile] = result
    check_active()

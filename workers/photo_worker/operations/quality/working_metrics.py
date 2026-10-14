"""Bounded technical metrics for a geometry-changing master, no pixel baseline.

Matches legacy clipping/Laplacian definitions without clipping the source RGB.
Only measurement gray tiles are clipped, as in the existing quality policy.
"""
import cv2
import numpy as np
from photo_worker.contracts.quality import TechnicalMetrics


def measure_working_metrics(rgb, alpha=None, *, check_active=lambda: None, tile_edge=256):
    if (getattr(rgb, 'dtype', None) != np.dtype('float32') or rgb.ndim != 3
            or rgb.shape[2] != 3 or min(rgb.shape[:2]) < 1
            or rgb.shape[0]*rgb.shape[1] > 200000000
            or type(tile_edge) is not int or not 16 <= tile_edge <= 1024):
        raise ValueError('QUALITY_WORKING_LAYOUT_INVALID')
    h, w, _ = rgb.shape
    if alpha is not None and (alpha.shape != (h, w)
            or alpha.dtype not in (np.dtype('float32'), np.dtype('uint16'))):
        raise ValueError('QUALITY_ALPHA_LAYOUT_INVALID')
    below = above = black = white = visible_channels = 0
    lap_sum = lap_squared = 0.
    for top in range(0, h, tile_edge):
        for left in range(0, w, tile_edge):
            check_active()
            bottom, right = min(h, top+tile_edge), min(w, left+tile_edge)
            tile = np.asarray(rgb[top:bottom, left:right])
            if not np.isfinite(tile).all(): raise ValueError('QUALITY_NONFINITE')
            below += int(np.count_nonzero(tile < 0))
            above += int(np.count_nonzero(tile > 1))
            if alpha is None:
                selected = tile.reshape(-1)
            else:
                opacity = np.asarray(alpha[top:bottom, left:right])
                if alpha.dtype == np.dtype('float32') and (not np.isfinite(opacity).all()
                        or np.any(opacity < 0) or np.any(opacity > 1)):
                    raise ValueError('QUALITY_ALPHA_RANGE_INVALID')
                selected = tile[opacity > 0].reshape(-1)
            visible_channels += selected.size
            black += int(np.count_nonzero(selected <= 1/65535))
            white += int(np.count_nonzero(selected >= 1-1/65535))
            # One-pixel halo preserves interior Laplacian boundaries. OpenCV
            # reflects only at physical image edges, never at a tile edge.
            y0, y1, x0, x1 = max(0, top-1), min(h, bottom+1), max(0, left-1), min(w, right+1)
            halo = np.asarray(rgb[y0:y1, x0:x1])
            if not np.isfinite(halo).all(): raise ValueError('QUALITY_NONFINITE')
            gray = np.mean(np.clip(halo, 0, 1), axis=2).astype(np.float32)
            lap = cv2.Laplacian(gray, cv2.CV_32F)[top-y0:bottom-y0, left-x0:right-x0]
            values = lap.astype(np.float64)
            lap_sum += float(np.sum(values))
            lap_squared += float(np.sum(values*values))
    check_active()
    count = h*w
    return TechnicalMetrics(below_zero_fraction=below/(count*3), above_one_fraction=above/(count*3),
        clipped_black_fraction=black/visible_channels if visible_channels else 0.,
        clipped_white_fraction=white/visible_channels if visible_channels else 0.,
        sharpness_laplacian=max(0., lap_squared/count-(lap_sum/count)**2),
        seam_p99_delta=0., finite=True)

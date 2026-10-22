"""Bounded conservative mask presentation; never modifies editing masks."""
import numpy as np


def prepare_preview_mask_overlay(mask, *, width, height, protected, max_bytes, check_active):
    """RGBA display overlay, using maximum coverage over each output cell.

    Tiny protected/affected regions remain visible when downsampling. This is
    display-only; no output is eligible as an editing mask or quality evidence.
    """
    if (not callable(check_active) or type(protected) is not bool
            or type(width) is not int or type(height) is not int
            or not 1 <= width <= 4096 or not 1 <= height <= 4096
            or not isinstance(mask, np.ndarray) or mask.dtype != np.float32
            or mask.ndim != 2 or min(mask.shape) < 1
            or mask.size > 200_000_000
            or width > mask.shape[1] or height > mask.shape[0]
            or type(max_bytes) is not int or max_bytes > 2**53-1):
        raise ValueError('SEMANTIC_PREVIEW_OVERLAY_INVALID')
    required = mask.nbytes + 4*width*height + 64*mask.shape[1] + 16*1024**2
    if max_bytes < required:
        raise ValueError('SEMANTIC_PREVIEW_OVERLAY_BUDGET')
    # Validation temporaries remain bounded by row width, not total pixels.
    for row in range(mask.shape[0]):
        check_active()
        values = mask[row]
        if not np.isfinite(values).all() or np.any(values < 0) or np.any(values > 1):
            raise ValueError('SEMANTIC_PREVIEW_OVERLAY_INVALID')
    check_active()
    output = np.zeros((height, width, 4), dtype=np.uint8)
    output[:, :, :3] = (0, 220, 255) if protected else (255, 80, 40)
    starts = np.arange(width, dtype=np.int64)*mask.shape[1]//width
    for row in range(height):
        check_active()
        first = row*mask.shape[0]//height
        last = (row+1)*mask.shape[0]//height
        coverage = np.max(mask[first:last], axis=0)
        coverage = np.maximum.reduceat(coverage, starts)
        output[row, :, 3] = np.ceil(coverage*255).astype(np.uint8)
    check_active()
    return output


def write_preview_mask_overlay(path, rgba, *, max_bytes, max_output_bytes, check_active):
    """Encode a caller-owned scratch PNG and verify exact decoded RGBA.

    No photo ICC profile belongs on these display mask colors. Nothing here
    publishes an artifact or confers producer/quality/Apply authority.
    """
    from pathlib import Path
    from PIL import Image
    if (not callable(check_active) or not isinstance(path, Path) or path.suffix != '.png'
            or not isinstance(rgba, np.ndarray) or rgba.dtype != np.uint8
            or rgba.ndim != 3 or rgba.shape[2] != 4
            or not 1 <= rgba.shape[0] <= 4096 or not 1 <= rgba.shape[1] <= 4096
            or type(max_bytes) is not int or not 1 <= max_bytes <= 2**53-1
            or type(max_output_bytes) is not int or not 1 <= max_output_bytes <= 2**53-1):
        raise ValueError('SEMANTIC_PREVIEW_OVERLAY_INVALID')
    pixels = rgba.shape[0]*rgba.shape[1]
    # Own input, encoder and verification arrays plus conservative codec cost.
    if max_bytes < 24*pixels + 16*1024**2:
        raise ValueError('SEMANTIC_PREVIEW_OVERLAY_BUDGET')
    # Reserve a worst-case uncompressed PNG bound before encoder IO.
    if max_output_bytes < 5*pixels + 65536:
        raise ValueError('SEMANTIC_PREVIEW_OVERLAY_BUDGET')
    check_active()
    owned = rgba.copy(order='C')
    with Image.fromarray(owned, mode='RGBA') as encoded:
        encoded.save(path, format='PNG', compress_level=6, optimize=False)
    check_active()
    size = path.stat().st_size
    if not 1 <= size <= max_output_bytes:
        raise ValueError('SEMANTIC_PREVIEW_OVERLAY_OUTPUT_INVALID')
    with Image.open(path) as encoded:
        if (encoded.format != 'PNG' or encoded.mode != 'RGBA'
                or encoded.size != (owned.shape[1], owned.shape[0]) or encoded.info):
            raise ValueError('SEMANTIC_PREVIEW_OVERLAY_OUTPUT_INVALID')
        decoded = np.asarray(encoded)
        for row in range(decoded.shape[0]):
            check_active()
            if not np.array_equal(decoded[row], owned[row]):
                raise ValueError('SEMANTIC_PREVIEW_OVERLAY_OUTPUT_INVALID')
    check_active()
    return size

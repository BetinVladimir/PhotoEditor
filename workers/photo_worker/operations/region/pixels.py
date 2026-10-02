"""Extract a bounded loupe only after the full-canvas recipe has rendered."""
from collections.abc import Callable

import numpy as np

from photo_worker.operations.export.encode import _resize_rgba
from .contracts import RegionPreviewParams


def extract_rendered_region(params: RegionPreviewParams, source_body: bytes,
                            rendered_rgb: np.ndarray,
                            rendered_alpha: np.ndarray | None = None, *,
                            check_active: Callable[[], None] = lambda: None,
                            ) -> tuple[np.ndarray, np.ndarray | None]:
    """Preserve existing area/premultiplied resize and reject partial canvases.

    Caller must render the entire frozen recipe through its qualified pipeline,
    preserving the source working encoding. This function neither substitutes a
    partial recipe renderer nor proves render provenance/resource admission.
    Returned arrays own their storage and never alias full-canvas scratch maps.
    """
    check_active()
    image = params.validate_source(source_body)
    shape = (image.canvas.height, image.canvas.width)
    if (not isinstance(rendered_rgb, np.ndarray)
            or rendered_rgb.shape != (*shape, 3)
            or rendered_rgb.dtype != np.float32
            or (image.alpha is None) != (rendered_alpha is None)):
        raise ValueError('REGION_RENDER_INVALID')
    if rendered_alpha is not None and (not isinstance(rendered_alpha, np.ndarray)
            or rendered_alpha.shape != shape or rendered_alpha.dtype != np.float32):
        raise ValueError('REGION_RENDER_INVALID')
    # Scan tiles to avoid whole-canvas Boolean temporaries and observe leases.
    for y in range(0, shape[0], 512):
        for x in range(0, shape[1], 512):
            check_active()
            values = [rendered_rgb[y:y+512, x:x+512]]
            if rendered_alpha is not None:
                values.append(rendered_alpha[y:y+512, x:x+512])
            for value in values:
                if not np.isfinite(value).all() or np.any((value < 0) | (value > 1)):
                    raise ValueError('REGION_RENDER_INVALID')
    geometry = params.geometry
    rows = slice(geometry.top, geometry.top + geometry.height)
    cols = slice(geometry.left, geometry.left + geometry.width)
    rgb = rendered_rgb[rows, cols]
    alpha = rendered_alpha[rows, cols] if rendered_alpha is not None else None
    check_active()
    rgb, alpha = _resize_rgba(rgb, alpha, geometry.output_width,
        geometry.output_height, encoding=image.color.encoding)
    check_active()
    return (np.array(rgb, dtype=np.float32, copy=True, order='C'),
            np.array(alpha, dtype=np.float32, copy=True, order='C')
            if alpha is not None else None)

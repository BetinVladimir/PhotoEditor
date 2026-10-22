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


def extract_projected_rendered_region(params,source_body,rendered_rgb,rendered_alpha,*,
        target_canvas,target_chain,load_inverse_field,check_active=lambda:None):
    """Crop source ROI in actual processed coordinates, then presentation resize.

    This path requires the caller's independently authenticated executed geometry
    descriptor. It does not accept a target size as a substitute for provenance.
    Returned geometry is explicit and must be bound in the result manifest and
    independently recomputed by adoption before this path can be registered.
    """
    import math
    from .projection import ProjectedBounds,project_region_chain
    from .contracts import RegionGeometry
    source=params.validate_source(source_body)
    if target_canvas.source_sha256!=source.canvas.source_sha256:
        raise ValueError('REGION_PROJECTION_SOURCE_INVALID')
    bounds=project_region_chain(ProjectedBounds(params.geometry.left,params.geometry.top,
        params.geometry.width,params.geometry.height),source.canvas,target_canvas,
        source.transform_chain,target_chain,load_inverse_field=load_inverse_field,
        check_active=check_active)
    height,width=target_canvas.height,target_canvas.width
    if (height*width>200_000_000 or not isinstance(rendered_rgb,np.ndarray)
            or rendered_rgb.dtype!=np.float32 or rendered_rgb.shape!=(height,width,3)
            or params.execution is None or params.execution.stages[-1].target_has_alpha!=(rendered_alpha is not None)):
        raise ValueError('REGION_RENDER_INVALID')
    if rendered_alpha is not None and (not isinstance(rendered_alpha,np.ndarray)
            or rendered_alpha.dtype!=np.float32 or rendered_alpha.shape!=(height,width)):
        raise ValueError('REGION_RENDER_INVALID')
    for y in range(0,height,512):
        for x in range(0,width,512):
            check_active()
            values=[rendered_rgb[y:y+512,x:x+512]]
            if rendered_alpha is not None:values.append(rendered_alpha[y:y+512,x:x+512])
            if any(not np.isfinite(v).all() or np.any((v<0)|(v>1)) for v in values):
                raise ValueError('REGION_RENDER_INVALID')
    scale=min(1.,params.request.max_edge/max(bounds.width,bounds.height))
    geometry=RegionGeometry(left=bounds.left,top=bounds.top,width=bounds.width,height=bounds.height,
        output_width=max(1,math.floor(bounds.width*scale)),output_height=max(1,math.floor(bounds.height*scale)))
    rows=slice(bounds.top,bounds.top+bounds.height);cols=slice(bounds.left,bounds.left+bounds.width)
    rgb,alpha=_resize_rgba(rendered_rgb[rows,cols],None if rendered_alpha is None else rendered_alpha[rows,cols],
        geometry.output_width,geometry.output_height,encoding=source.color.encoding)
    check_active()
    return (np.array(rgb,dtype=np.float32,copy=True,order='C'),
        None if alpha is None else np.array(alpha,dtype=np.float32,copy=True,order='C'),geometry)

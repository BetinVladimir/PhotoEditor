"""Attempt-owned full-canvas mappings for a complete region renderer."""
from contextlib import ExitStack, contextmanager
from dataclasses import dataclass
import numpy as np
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.operations.optical.alpha import normalized_alpha


@dataclass(frozen=True)
class RegionSourceArrays:
    rgb: np.ndarray
    alpha: np.ndarray | None


@contextmanager
def prepare_region_source(params, source_body, artifacts, *, alpha_mode,
                          max_normalization_bytes, check_active=lambda: None,
                          execution_profile=None):
    """Map the byte-bound current WorkingImage before any ROI crop.

    Pixels already occupy the current canvas. Its inherited transform chain is
    provenance, not a command to resample these pixels again. A complete renderer
    must separately handle new recipe geometry and style descendants. Resource
    admission precedes this helper; legacy alpha conversion requires an explicit
    policy and its full float32 scratch budget. Mappings cannot escape context.
    """
    check_active()
    source = params.validate_source(source_body)
    h, w = source.canvas.height, source.canvas.width
    if alpha_mode not in ('require_float32', 'normalize_uint16'):
        raise ValueError('REGION_ALPHA_MODE_INVALID')
    if type(max_normalization_bytes) is not int or not 0<=max_normalization_bytes<=2**53-1:
        raise ValueError('REGION_ALPHA_BUDGET_INVALID')
    if (source.alpha is not None and alpha_mode == 'normalize_uint16'
            and max_normalization_bytes < h*w*4+4096):
        raise ValueError('REGION_ALPHA_BUDGET_INVALID')
    if execution_profile is not None:
        from .resources import admit_region_source
        admit_region_source(execution_profile,w,h,has_alpha=source.alpha is not None,
                            alpha_mode=alpha_mode)
    with ExitStack() as stack:
        pixels = stack.enter_context(artifacts.input(source.pixels, suffix='.npy'))
        # Finite scene values can exceed display range before recipe rendering.
        rgb = stack.enter_context(read_array(pixels.path, (h,w,3), 'float32', check_active))
        alpha = None
        if source.alpha is not None:
            local = stack.enter_context(artifacts.input(source.alpha, suffix='.npy'))
            mapped = stack.enter_context(read_alpha_array(local.path, (h,w), check_active)
                if alpha_mode == 'normalize_uint16' else
                read_array(local.path, (h,w), 'float32', check_active, unit_interval=True))
            alpha = stack.enter_context(normalized_alpha(mapped, (h,w), artifacts.scratch,
                                                       tile=512, check_active=check_active))
        check_active()
        yield RegionSourceArrays(rgb, alpha)

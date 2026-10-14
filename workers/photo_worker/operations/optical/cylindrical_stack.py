"""Retained cylindrical projections followed by V29 automatic panorama."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
import numpy as np
from photo_worker.algorithms.multi_registration import _shapes
from .cylindrical_prepare import prepare_cylindrical_frame, cylindrical_resources, CylindricalFiles
from .multi_prepare import prepare_automatic_planar_stack, AutomaticMultiPrepared, _motion_parameters


@dataclass(frozen=True)
class CylindricalPanoramaPrepared:
    projections: tuple[CylindricalFiles, ...]
    panorama: AutomaticMultiPrepared


def cylindrical_projection_resources(shapes, *, tile=256):
    """First-stage aggregate budget; panorama canvas is unknown until matching."""
    owned = _shapes(shapes)
    items = [cylindrical_resources(shape, tile=tile) for shape in owned]
    retained = sum(item['scratch_bytes'] for item in items)
    # Retain every original and every projected page while processing the next.
    ram = 16*sum(h*w for h,w in owned)+retained+max(item['ram_bytes'] for item in items)
    if max(ram, retained) > 2**53-1:
        raise ValueError('CYLINDRICAL_RESOURCE_INVALID')
    return dict(ram_bytes=ram, scratch_bytes=retained, vram_bytes=0)


@contextmanager
def prepare_cylindrical_panorama(frames, alphas, reference, scratch, *, max_pixels,
        ram_max_bytes, registration_error_px, match_inlier_min, motion_threshold,
        luma, tile=256, check_active=lambda: None):
    """Two-stage admission; never drop a projection or nonlinear lineage field.

    Projection admission precedes any file. After matching determines output
    bounds, automatic panorama admits the additional complete stack against the
    remaining RAM/disk budgets. Any failure unwinds every projection file.
    """
    if (not isinstance(frames,(list,tuple)) or not isinstance(alphas,(list,tuple))
            or not 2<=len(frames)<=20 or len(alphas)!=len(frames)
            or type(reference) is not int or not 0<=reference<len(frames)
            or type(max_pixels) is not int or not 1<=max_pixels<=200000000):
        raise ValueError('CYLINDRICAL_INPUT_INVALID')
    _motion_parameters('panorama', motion_threshold, 'review')
    for rgb, alpha in zip(frames,alphas):
        if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[2]!=3
                or not isinstance(alpha,np.ndarray) or alpha.dtype!=np.float32 or alpha.shape!=rgb.shape[:2]):
            raise ValueError('CYLINDRICAL_INPUT_INVALID')
    required=cylindrical_projection_resources([a.shape for a in alphas],tile=tile)
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:
        raise ValueError('CYLINDRICAL_RAM_INVALID')
    if ram_max_bytes<=required['ram_bytes']:
        raise ValueError('CYLINDRICAL_RAM_EXCEEDED')
    if scratch.budget_bytes<=required['scratch_bytes']:
        raise ValueError('CYLINDRICAL_SCRATCH_EXCEEDED')
    # Delegate reservations to the original attempt owner, while exposing only
    # remaining capacity to the downstream aggregate admission calculation.
    class RemainingScratch:
        budget_bytes=scratch.budget_bytes-required['scratch_bytes']
        def reserve(self,size):return scratch.reserve(size)
    check_active()
    with ExitStack() as lifetime:
        projections=[]; colors=[]; opacity=[]
        def reopen(path):
            value=np.load(path,mmap_mode='r',allow_pickle=False)
            lifetime.callback(value._mmap.close)
            return value
        for rgb,alpha in zip(frames,alphas):
            projected=lifetime.enter_context(prepare_cylindrical_frame(rgb,alpha,scratch,
                ram_max_bytes=ram_max_bytes,tile=tile,check_active=check_active))
            projections.append(projected)
            colors.append(reopen(projected.rgb));opacity.append(reopen(projected.alpha))
        automatic=lifetime.enter_context(prepare_automatic_planar_stack(colors,opacity,reference,RemainingScratch(),
            operation='panorama',max_pixels=max_pixels,ram_max_bytes=ram_max_bytes-required['ram_bytes'],
            registration_error_px=registration_error_px,match_inlier_min=match_inlier_min,
            motion_threshold=motion_threshold,luma=luma,tile=tile,check_active=check_active))
        check_active()
        yield CylindricalPanoramaPrepared(tuple(projections),automatic)

"""Aggregate planar V29 preparation; registered matrices need external authority.

No operation is enabled by this helper. A qualified adapter must supply exact
source order, ICC-verified linear inputs, accepted registrations and lineage.
"""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from math import isfinite
import numpy as np
from photo_worker.algorithms.multi_registration import _matrix, _shapes
from photo_worker.algorithms.panorama_geometry import plan_panorama_canvas
from .registered_frame import prepare_registered_frame, registered_frame_resources, RegisteredFrameFiles
from .panorama_prepare import prepare_panorama_blend, panorama_blend_resources, PanoramaFiles
from .focus_prepare import prepare_focus_stack, focus_resources, FocusFiles


@dataclass(frozen=True)
class MultiPrepared:
    operation: str
    shape: tuple[int, int]
    reference: int
    registrations: tuple[RegisteredFrameFiles, ...]
    result: PanoramaFiles | FocusFiles


def multi_planar_resources(source_shapes, output_shape, operation, *, tile=256):
    shapes = _shapes(source_shapes)
    if operation not in ('panorama', 'focus_stack') or operation=='panorama' and len(shapes)>20:
        raise ValueError('MULTI_OPERATION_INVALID')
    registrations = [registered_frame_resources(s, output_shape, tile=tile) for s in shapes]
    stage = (panorama_blend_resources(output_shape, len(shapes), tile=tile) if operation == 'panorama'
             else focus_resources(output_shape, len(shapes), tile=tile))
    retained = sum(r['scratch_bytes'] for r in registrations)
    originals = 16*sum(h*w for h, w in shapes)
    # Retain all original and registration pages while the final stage runs.
    # The stage already includes visible registered RGB/alpha/coverage; counting
    # them again here is conservative, never a claim of measured mmap residency.
    ram = originals+retained+stage['ram_bytes']+256*tile*tile+256*1024*1024
    scratch = retained+stage['scratch_bytes']
    if max(ram, scratch) > 2**53-1:
        raise ValueError('MULTI_RESOURCE_INVALID')
    return dict(pixel_count=output_shape[0]*output_shape[1], ram_bytes=ram,
                scratch_bytes=scratch, output_bytes=stage['output_bytes'], vram_bytes=0)


def _motion_parameters(operation, threshold, policy):
    # Refuse malformed policy before projection, registration or scratch I/O.
    if (type(threshold) not in (int, float) or not isfinite(threshold) or threshold < 0
            or operation == 'focus_stack' and policy not in ('reject', 'review')):
        raise ValueError('MULTI_MOTION_INVALID')


@contextmanager
def prepare_planar_stack(frames, alphas, matrices, reference, scratch, *, operation,
                         max_pixels, ram_max_bytes, motion_threshold, luma,
                         motion_policy='review', tile=256, check_active=lambda: None):
    if (not isinstance(frames, (list, tuple)) or not isinstance(alphas, (list, tuple))
            or not isinstance(matrices, (list, tuple)) or not 2 <= len(frames) <= 30
            or len(alphas) != len(frames) or len(matrices) != len(frames)
            or operation == 'panorama' and len(frames) > 20
            or type(reference) is not int or not 0 <= reference < len(frames)
            or type(max_pixels) is not int or not 1 <= max_pixels <= 200000000):
        raise ValueError('MULTI_INPUT_INVALID')
    _motion_parameters(operation, motion_threshold, motion_policy)
    for rgb, alpha in zip(frames, alphas):
        if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
                or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]):
            raise ValueError('MULTI_INPUT_INVALID')
    shapes = _shapes([a.shape for a in alphas])
    owned = tuple(_matrix(h, s) for h, s in zip(matrices, shapes))
    if not np.array_equal(owned[reference], np.eye(3)):
        raise ValueError('MULTI_REFERENCE_INVALID')
    if operation == 'panorama':
        canvas = plan_panorama_canvas(owned, shapes, max_pixels=max_pixels, check_active=check_active)
        output_shape = (canvas.height, canvas.width); owned = canvas.source_to_output
    elif operation == 'focus_stack':
        output_shape = shapes[reference]
        if output_shape[0]*output_shape[1] > max_pixels:
            raise ValueError('MULTI_PIXEL_LIMIT')
    else:
        raise ValueError('MULTI_OPERATION_INVALID')
    required = multi_planar_resources(shapes, output_shape, operation, tile=tile)
    if type(ram_max_bytes) is not int or not 1 <= ram_max_bytes <= 2**53-1:
        raise ValueError('MULTI_RAM_INVALID')
    if ram_max_bytes < required['ram_bytes']:
        raise ValueError('MULTI_RAM_EXCEEDED')
    if scratch.budget_bytes < required['scratch_bytes']:
        raise ValueError('MULTI_SCRATCH_EXCEEDED')
    check_active()
    with ExitStack() as lifetime:
        registrations = []; colors = []; opacity = []; coverage = []
        def reopen(path):
            value = np.load(path, mmap_mode='r', allow_pickle=False)
            lifetime.callback(value._mmap.close)
            return value
        for rgb, alpha, h in zip(frames, alphas, owned):
            check_active()
            prepared = lifetime.enter_context(prepare_registered_frame(rgb, alpha, h, output_shape,
                scratch, ram_max_bytes=ram_max_bytes, tile=tile, check_active=check_active))
            registrations.append(prepared)
            colors.append(reopen(prepared.rgb)); opacity.append(reopen(prepared.alpha)); coverage.append(reopen(prepared.coverage))
        if operation == 'panorama':
            result = lifetime.enter_context(prepare_panorama_blend(colors, opacity, coverage, scratch,
                motion_threshold=motion_threshold, ram_max_bytes=ram_max_bytes, tile=tile, check_active=check_active))
        else:
            result = lifetime.enter_context(prepare_focus_stack(colors, opacity, coverage, reference, scratch,
                luma=luma, motion_threshold=motion_threshold, motion_policy=motion_policy,
                ram_max_bytes=ram_max_bytes, tile=tile, check_active=check_active))
        check_active()
        yield MultiPrepared(operation, output_shape, reference, tuple(registrations), result)


@dataclass(frozen=True)
class AutomaticMultiPrepared:
    prepared: MultiPrepared
    pairs: tuple
    registration_inconsistent: bool


@contextmanager
def prepare_automatic_planar_stack(frames, alphas, reference, scratch, *, operation,
        max_pixels, ram_max_bytes, registration_error_px, match_inlier_min,
        motion_threshold, luma, motion_policy='review', tile=256, check_active=lambda: None):
    """Derive every matrix from current pixels; no caller matrix substitution.

    Current source/ICC/policy authorization and runtime qualification are still
    mandatory at the operation adapter. Registration inconsistency is retained
    as review evidence, never silently dropped.
    """
    from photo_worker.algorithms.multi_proxy import linear_registration_proxy
    from photo_worker.algorithms.multi_registration import register_pairs, assemble_registrations
    if (operation not in ('panorama', 'focus_stack') or not isinstance(frames, (list, tuple))
            or not isinstance(alphas, (list, tuple)) or not 2 <= len(frames) <= 30
            or len(alphas) != len(frames) or operation == 'panorama' and len(frames) > 20
            or type(reference) is not int or not 0 <= reference < len(frames)
            or type(max_pixels) is not int or not 1 <= max_pixels <= 200000000):
        raise ValueError('MULTI_INPUT_INVALID')
    _motion_parameters(operation, motion_threshold, motion_policy)
    shapes = []
    for rgb, alpha in zip(frames, alphas):
        if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
                or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]):
            raise ValueError('MULTI_INPUT_INVALID')
        shapes.append(alpha.shape)
    shapes = _shapes(shapes)
    # Retained uint8 proxies/masks, their owned matching copies, one resized RGB
    # and alpha pair, feature/matching workspace and original mapped inputs.
    proxy_ram = multi_proxy_ram(shapes)
    if type(ram_max_bytes) is not int or not 1 <= ram_max_bytes <= 2**53-1:
        raise ValueError('MULTI_RAM_INVALID')
    if ram_max_bytes <= proxy_ram:
        raise ValueError('MULTI_RAM_EXCEEDED')
    check_active()
    proxies = [linear_registration_proxy(rgb, alpha, luma=luma, check_active=check_active)
               for rgb,alpha in zip(frames,alphas)]
    pairs = register_pairs([p.pixels for p in proxies], shapes,
        masks=[p.mask for p in proxies], registration_error_px=registration_error_px,
        match_inlier_min=match_inlier_min, reference=reference if operation=='focus_stack' else None,
        check_active=check_active)
    matrices, inconsistent = assemble_registrations(pairs, shapes, reference,
        direct=operation=='focus_stack', registration_error_px=registration_error_px, check_active=check_active)
    with prepare_planar_stack(frames, alphas, matrices, reference, scratch,
            operation=operation, max_pixels=max_pixels, ram_max_bytes=ram_max_bytes-proxy_ram,
            motion_threshold=motion_threshold, luma=luma, motion_policy=motion_policy,
            tile=tile, check_active=check_active) as prepared:
        yield AutomaticMultiPrepared(prepared, pairs, inconsistent)


def multi_proxy_ram(shapes):
    owned = _shapes(shapes)
    return 16*sum(h*w for h,w in owned)+(4*len(owned)+16)*2048**2+256*1024*1024

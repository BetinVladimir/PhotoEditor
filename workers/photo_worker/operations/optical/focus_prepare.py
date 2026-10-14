"""Attempt-owned disk-backed focus pyramid orchestration, before artifact staging.

Inputs are already registered, ICC-verified linear float32 RGB on the reference
canvas. This helper grants no source, qualification, adoption or Apply authority.
"""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.focus_pyramid import (
    pyramid_shapes, write_focus_weights, gaussian_downsample, bilinear_upsample,
    write_laplacian_band, blend_focus_level, reconstruct_focus_level,
)
from photo_worker.algorithms.focus_motion import detect_focus_motion, apply_focus_motion_policy
from photo_worker.runtime.scratch import Reservation


@dataclass(frozen=True)
class FocusFiles:
    rgb: Path
    alpha: Path
    motion: Path
    motion_fraction: float
    reservation: Reservation


def focus_scratch_bytes(shape, count):
    if type(count) is not int or not 2 <= count <= 30:
        raise ValueError('FOCUS_INPUT_INVALID')
    sizes = [h*w for h, w in pyramid_shapes(shape)]
    # Per input: Gaussian RGB coarse levels, focus weights and Laplacian bands.
    per_input = 24*sum(sizes[1:])+8*sum(sizes)+24*sum(sizes[:-1])
    # Shared expanded levels, blended pyramid, reconstructed pyramid, final files.
    shared = 24*sum(sizes[:-1])+48*sum(sizes)+17*sizes[0]
    arrays = count*((len(sizes)-1)+len(sizes)+(len(sizes)-1))+3*len(sizes)-1+3
    return count*per_input+shared+arrays*4096


def focus_resources(shape, count, *, tile=256):
    """Conservative CPU admission, including all live mapped pages.

    Registered input ownership/downloads and output lineage descriptors are
    separate adapter obligations. No unmeasured low-RAM mmap claim is made.
    """
    if type(tile) is not int or not 1 <= tile <= 512:
        raise ValueError('FOCUS_INPUT_INVALID')
    scratch = focus_scratch_bytes(shape, count)
    pixels = shape[0]*shape[1]
    # Input linear RGB+alpha+coverage; all scratch pages; float64 Gaussian
    # halos/gathers/folds and OpenCV temporaries; disk3 row cache; fixed headroom.
    ram = 17*count*pixels+scratch+256*(2*tile+10)**2+16*shape[1]+256*1024*1024
    output = 17*pixels+3*4096
    if max(ram, scratch, output) > 2**53-1:
        raise ValueError('FOCUS_RESOURCE_INVALID')
    return dict(pixel_count=pixels, ram_bytes=ram, scratch_bytes=scratch,
                output_bytes=output, vram_bytes=0)


@contextmanager
def prepare_focus_stack(frames, alphas, coverages, reference, scratch, *, luma,
                        motion_threshold, motion_policy, ram_max_bytes, tile=256, check_active=lambda: None):
    if (not isinstance(frames, (list, tuple)) or not frames
            or not isinstance(frames[0], np.ndarray) or frames[0].ndim != 3):
        raise ValueError('FOCUS_INPUT_INVALID')
    if (not isinstance(alphas, (list, tuple)) or not isinstance(coverages, (list, tuple))
            or not 2 <= len(frames) <= 30 or len(alphas) != len(frames)
            or len(coverages) != len(frames) or type(reference) is not int
            or not 0 <= reference < len(frames)):
        raise ValueError('FOCUS_INPUT_INVALID')
    shape = frames[0].shape[:2]; shapes = pyramid_shapes(shape)
    for rgb, alpha, coverage in zip(frames, alphas, coverages):
        if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.shape != (*shape, 3)
                or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != shape
                or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_ or coverage.shape != shape):
            raise ValueError('FOCUS_INPUT_INVALID')
    required = focus_resources(shape, len(frames), tile=tile)
    if type(ram_max_bytes) is not int or not 1 <= ram_max_bytes <= 2**53-1:
        raise ValueError('FOCUS_RAM_INVALID')
    if ram_max_bytes < required['ram_bytes']:
        raise ValueError('FOCUS_RAM_EXCEEDED')
    if motion_policy not in ('reject', 'review'):
        raise ValueError('FOCUS_MOTION_INVALID')
    try:
        coefficients = np.asarray(luma, dtype=np.float64)
    except (TypeError, ValueError) as exc:
        raise ValueError('FOCUS_WEIGHT_INVALID') from exc
    if (coefficients.shape != (3,) or not np.isfinite(coefficients).all()
            or np.any(coefficients < 0) or abs(float(coefficients.sum())-1) > 1e-6):
        raise ValueError('FOCUS_WEIGHT_INVALID')
    # Own the admitted coefficients before resource allocation and callbacks.
    luma = tuple(float(value) for value in coefficients)
    budget = required['scratch_bytes']; check_active()
    with scratch.reserve(budget) as reservation:
        with ExitStack() as mappings:
            serial = 0
            def allocate(name, size, dtype):
                nonlocal serial
                serial += 1
                path = reservation.path(f'focus-{serial}-{name}', '.npy')
                array = np.lib.format.open_memmap(path, mode='w+', dtype=dtype, shape=size)
                mappings.callback(array._mmap.close)
                return path, array
            motion_path, motion = allocate('motion', shape, np.bool_)
            fraction = detect_focus_motion(frames, alphas, coverages, reference, motion,
                motion_threshold=motion_threshold, tile=tile, check_active=check_active)
            if motion_policy == 'reject' and fraction > 0:
                raise ValueError('MOTION_DETECTED')
            expanded = [allocate('expanded', (*s, 3), '<f8')[1] for s in shapes[:-1]]
            frame_bands = []; frame_weights = []
            for rgb, alpha, coverage in zip(frames, alphas, coverages):
                check_active(); gaussians = [rgb]
                weights = [allocate('weight', s, '<f8')[1] for s in shapes]
                write_focus_weights(rgb, coverage, weights[0], luma=luma, alpha=alpha,
                                    tile=tile, check_active=check_active)
                for level, size in enumerate(shapes[1:]):
                    coarse = allocate('gaussian', (*size, 3), '<f8')[1]
                    gaussian_downsample(gaussians[-1], coarse, tile=tile, check_active=check_active)
                    gaussians.append(coarse)
                    gaussian_downsample(weights[level], weights[level+1], tile=tile, check_active=check_active)
                bands = []
                for level, size in enumerate(shapes[:-1]):
                    band = allocate('band', (*size, 3), '<f8')[1]
                    bilinear_upsample(gaussians[level+1], expanded[level], tile=tile, check_active=check_active)
                    write_laplacian_band(gaussians[level], expanded[level], band, tile=tile, check_active=check_active)
                    bands.append(band)
                bands.append(gaussians[-1]); frame_bands.append(bands); frame_weights.append(weights)
            blended = [allocate('blended', (*s, 3), '<f8')[1] for s in shapes]
            for level in range(len(shapes)):
                blend_focus_level([bands[level] for bands in frame_bands],
                    [weights[level] for weights in frame_weights], reference, blended[level],
                    tile=tile, check_active=check_active)
            reconstructed = [allocate('reconstructed', (*s, 3), '<f8')[1] for s in shapes]
            coarse_height, coarse_width = shapes[-1]
            for y in range(0, coarse_height, tile):
                for x in range(0, coarse_width, tile):
                    check_active()
                    reconstructed[-1][y:y+tile, x:x+tile] = blended[-1][y:y+tile, x:x+tile]
            for level in range(len(shapes)-2, -1, -1):
                bilinear_upsample(reconstructed[level+1], expanded[level], tile=tile, check_active=check_active)
                reconstruct_focus_level(blended[level], expanded[level], reconstructed[level],
                    tile=tile, check_active=check_active)
            rgb_path, colors = allocate('rgb', (*shape, 3), '<f4')
            alpha_path, opacity = allocate('alpha', shape, '<f4')
            height, width = shape
            for y in range(0, height, tile):
                for x in range(0, width, tile):
                    check_active(); region = np.s_[y:y+tile, x:x+tile]
                    # Focus output is on the reference canvas and retains its
                    # opacity. No-coverage pixels use exact reference color.
                    absent = np.ones(opacity[region].shape, bool)
                    for coverage, alpha in zip(coverages, alphas):
                        absent &= ~(coverage[region] & (alpha[region] > 0))
                    value = reconstructed[0][region].copy()
                    value[absent] = frames[reference][region][absent]
                    if not np.isfinite(value).all() or np.any(np.abs(value) > np.finfo(np.float32).max):
                        raise ValueError('FOCUS_OUTPUT_INVALID')
                    colors[region] = value; opacity[region] = alphas[reference][region]
            apply_focus_motion_policy(colors, opacity, frames[reference], alphas[reference], motion,
                policy=motion_policy, tile=tile, check_active=check_active)
            colors.flush(); opacity.flush(); motion.flush(); reservation.validate(); check_active()
        yield FocusFiles(rgb_path, alpha_path, motion_path, fraction, reservation)

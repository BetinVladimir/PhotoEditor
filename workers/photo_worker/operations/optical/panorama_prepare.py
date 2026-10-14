"""Admitted disk-backed panorama blend outputs before fenced publication."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.panorama_blend import blend_panorama
from photo_worker.runtime.scratch import Reservation


@dataclass(frozen=True)
class PanoramaFiles:
    rgb: Path
    alpha: Path
    motion: Path
    motion_fraction: float
    reservation: Reservation


def panorama_blend_resources(shape, count, *, tile=256):
    """Blend-stage budget; original/projection/registration inputs are separate."""
    if (not isinstance(shape, tuple) or len(shape) != 2
            or any(type(v) is not int or v < 1 for v in shape) or shape[0]*shape[1] > 200000000
            or type(count) is not int or not 2 <= count <= 20
            or type(tile) is not int or not 1 <= tile <= 512):
        raise ValueError('PANORAMA_RESOURCE_INVALID')
    pixels = shape[0]*shape[1]
    output = 17*pixels+3*4096
    # All warped RGB/alpha/coverage and output pages remain live. Native distance
    # transform, halo arrays and float64 folds are accounted conservatively.
    ram = 17*count*pixels+output+256*(tile+128)**2+16*shape[1]+256*1024*1024
    if max(ram, output) > 2**53-1:
        raise ValueError('PANORAMA_RESOURCE_INVALID')
    return dict(pixel_count=pixels, ram_bytes=ram, scratch_bytes=output,
                output_bytes=output, vram_bytes=0)


@contextmanager
def prepare_panorama_blend(frames, alphas, coverages, scratch, *, motion_threshold,
                           ram_max_bytes, tile=256, check_active=lambda: None):
    """Retain only closed NPY paths; remove all outputs on context exit/error.

    Inputs must already share a planned panorama canvas and verified linear ICC
    primaries. This stage does not accept a partial/disconnected registration.
    Report compilation must mark panorama review regardless of motion fraction.
    """
    if (not isinstance(frames, (list, tuple)) or not frames
            or not isinstance(frames[0], np.ndarray) or frames[0].ndim != 3):
        raise ValueError('PANORAMA_BLEND_INVALID')
    if (not isinstance(alphas, (list, tuple)) or not isinstance(coverages, (list, tuple))
            or not 2 <= len(frames) <= 20 or len(alphas) != len(frames)
            or len(coverages) != len(frames)):
        raise ValueError('PANORAMA_BLEND_INVALID')
    shape = frames[0].shape[:2]
    for rgb, alpha, coverage in zip(frames, alphas, coverages):
        if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.shape != (*shape, 3)
                or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != shape
                or not isinstance(coverage, np.ndarray) or coverage.dtype != np.bool_ or coverage.shape != shape):
            raise ValueError('PANORAMA_BLEND_INVALID')
    required = panorama_blend_resources(shape, len(frames), tile=tile)
    if type(ram_max_bytes) is not int or not 1 <= ram_max_bytes <= 2**53-1:
        raise ValueError('PANORAMA_RAM_INVALID')
    if ram_max_bytes < required['ram_bytes']:
        raise ValueError('PANORAMA_RAM_EXCEEDED')
    check_active()
    with scratch.reserve(required['scratch_bytes']) as reservation:
        with ExitStack() as mappings:
            def allocate(role, size, dtype):
                path = reservation.path('panorama-'+role, '.npy')
                value = np.lib.format.open_memmap(path, mode='w+', dtype=dtype, shape=size)
                mappings.callback(value._mmap.close)
                return path, value
            rgb_path, rgb = allocate('rgb', (*shape, 3), '<f4')
            alpha_path, alpha = allocate('alpha', shape, '<f4')
            motion_path, motion = allocate('motion', shape, np.bool_)
            fraction = blend_panorama(frames, alphas, coverages, rgb, alpha, motion,
                motion_threshold=motion_threshold, tile=tile, check_active=check_active)
            rgb.flush(); alpha.flush(); motion.flush(); reservation.validate(); check_active()
        yield PanoramaFiles(rgb_path, alpha_path, motion_path, fraction, reservation)

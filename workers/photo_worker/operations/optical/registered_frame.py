"""Attempt-scoped planar registered frame preparation for V29 multi-inputs."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.panorama_geometry import write_registration_inverse
from photo_worker.algorithms.multi_registration import _matrix
from photo_worker.algorithms.multi_sampling import sample_inverse_field
from photo_worker.runtime.scratch import Reservation


@dataclass(frozen=True)
class RegisteredFrameFiles:
    rgb: Path
    alpha: Path
    coverage: Path
    inverse_field: Path
    source_to_output: np.ndarray
    reservation: Reservation


def registered_frame_resources(source_shape, output_shape, *, tile=256):
    for shape in (source_shape, output_shape):
        if (not isinstance(shape, tuple) or len(shape) != 2
                or any(type(v) is not int or v < 1 for v in shape) or shape[0]*shape[1] > 200000000):
            raise ValueError('REGISTERED_FRAME_INVALID')
    if type(tile) is not int or not 1 <= tile <= 512:
        raise ValueError('REGISTERED_FRAME_INVALID')
    source = source_shape[0]*source_shape[1]; output = output_shape[0]*output_shape[1]
    scratch = 25*output+4*4096
    return dict(pixel_count=output, ram_bytes=16*source+scratch+256*tile*tile+256*1024*1024,
                scratch_bytes=scratch, output_bytes=17*output+3*4096, vram_bytes=0)


@contextmanager
def prepare_registered_frame(rgb, alpha, matrix, output_shape, scratch, *, ram_max_bytes,
                             tile=256, check_active=lambda: None):
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
            or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]):
        raise ValueError('REGISTERED_FRAME_INVALID')
    required = registered_frame_resources(alpha.shape, output_shape, tile=tile)
    owned = _matrix(matrix, alpha.shape)
    if type(ram_max_bytes) is not int or not 1 <= ram_max_bytes <= 2**53-1:
        raise ValueError('REGISTERED_FRAME_RAM_INVALID')
    if ram_max_bytes < required['ram_bytes']:
        raise ValueError('REGISTERED_FRAME_RAM_EXCEEDED')
    check_active()
    with scratch.reserve(required['scratch_bytes']) as reservation:
        with ExitStack() as mappings:
            def allocate(name, size, dtype):
                path = reservation.path('registered-'+name, '.npy')
                value = np.lib.format.open_memmap(path, mode='w+', dtype=dtype, shape=size)
                mappings.callback(value._mmap.close)
                return path, value
            rgb_path, colors = allocate('rgb', (*output_shape, 3), '<f4')
            alpha_path, opacity = allocate('alpha', output_shape, '<f4')
            field_path, field = allocate('field', (*output_shape, 2), '<f4')
            coverage_path, coverage = allocate('coverage', output_shape, np.bool_)
            write_registration_inverse(owned, alpha.shape, field, coverage, tile=tile, check_active=check_active)
            for block in sample_inverse_field(rgb, alpha, field, coverage, tile=tile, check_active=check_active):
                h, w = block.alpha.shape; region = np.s_[block.y:block.y+h, block.x:block.x+w]
                colors[region] = block.rgb; opacity[region] = block.alpha; coverage[region] = block.coverage
            colors.flush(); opacity.flush(); field.flush(); coverage.flush()
            reservation.validate(); check_active()
        yield RegisteredFrameFiles(rgb_path, alpha_path, coverage_path, field_path, owned, reservation)

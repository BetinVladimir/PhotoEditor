"""Retained V29 cylindrical projection files; never disguised as homography."""
from contextlib import contextmanager, ExitStack
from dataclasses import dataclass
from pathlib import Path
import numpy as np
from photo_worker.algorithms.panorama_geometry import write_cylindrical_inverse
from photo_worker.algorithms.multi_sampling import sample_inverse_field
from photo_worker.runtime.scratch import Reservation
from .registered_frame import registered_frame_resources


@dataclass(frozen=True)
class CylindricalFiles:
    rgb: Path
    alpha: Path
    coverage: Path
    inverse_field: Path
    reservation: Reservation


def cylindrical_resources(shape, *, tile=256):
    required = registered_frame_resources(shape, shape, tile=tile)
    # Projection writer processes at most64 full-width rows, using float64
    # coordinates and boolean coverage. Include those buffers explicitly.
    required['ram_bytes'] += 64*shape[1]*64
    return required


@contextmanager
def prepare_cylindrical_frame(rgb, alpha, scratch, *, ram_max_bytes,
                              tile=256, check_active=lambda: None):
    """Project into same-size linear canvas, with admitted retained dense map.

    Adapter must retain inverse_field as DenseWarpArray (half_pixel marker) for
    every source. Registration matrices refer to this projected canvas; they do
    not describe the original nonlinear geometry without the retained field.
    """
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or rgb.ndim != 3 or rgb.shape[2] != 3
            or not isinstance(alpha, np.ndarray) or alpha.dtype != np.float32 or alpha.shape != rgb.shape[:2]):
        raise ValueError('CYLINDRICAL_INPUT_INVALID')
    shape = alpha.shape; required = cylindrical_resources(shape, tile=tile)
    if type(ram_max_bytes) is not int or not 1 <= ram_max_bytes <= 2**53-1:
        raise ValueError('CYLINDRICAL_RAM_INVALID')
    if ram_max_bytes < required['ram_bytes']:
        raise ValueError('CYLINDRICAL_RAM_EXCEEDED')
    check_active()
    with scratch.reserve(required['scratch_bytes']) as reservation:
        with ExitStack() as mappings:
            def allocate(role, size, dtype):
                path = reservation.path('cylindrical-'+role, '.npy')
                value = np.lib.format.open_memmap(path, mode='w+', dtype=dtype, shape=size)
                mappings.callback(value._mmap.close)
                return path, value
            rgb_path, colors = allocate('rgb', (*shape, 3), '<f4')
            alpha_path, opacity = allocate('alpha', shape, '<f4')
            field_path, field = allocate('field', (*shape, 2), '<f4')
            coverage_path, coverage = allocate('coverage', shape, np.bool_)
            write_cylindrical_inverse(field, coverage, tile_rows=64, check_active=check_active)
            for block in sample_inverse_field(rgb, alpha, field, coverage, tile=tile, check_active=check_active):
                h, w = block.alpha.shape; region = np.s_[block.y:block.y+h, block.x:block.x+w]
                colors[region] = block.rgb; opacity[region] = block.alpha; coverage[region] = block.coverage
            for value in (colors, opacity, field, coverage): value.flush()
            reservation.validate(); check_active()
        yield CylindricalFiles(rgb_path, alpha_path, coverage_path, field_path, reservation)

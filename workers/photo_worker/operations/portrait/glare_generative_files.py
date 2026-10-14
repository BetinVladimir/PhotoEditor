"""V28 donorless glare full-frame output with explicit synthetic evidence."""

from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path

import numpy as np

from .glare_generative_patch import GenerativeGlarePatch


@dataclass(frozen=True)
class GenerativeGlareFiles:
    rgb: Path
    changed: Path
    generated: Path
    reservation: object
    clipping_fraction: float


@contextmanager
def write_generative_glare_files(source_rgb, patch, origin, scratch, *,
                                  target, protected, check_active):
    """Copy source exactly, overlay one bounded patch and prove mask support."""
    source = np.asarray(source_rgb)
    if (not isinstance(patch, GenerativeGlarePatch)
            or not callable(check_active) or source.dtype != np.float32
            or source.ndim != 3 or source.shape[2] != 3
            or min(source.shape[:2]) < 1
            or source.shape[0] * source.shape[1] > 200_000_000
            or not isinstance(origin, tuple) or len(origin) != 2
            or any(type(n) is not int or n < 0 for n in origin)):
        raise ValueError('GLARE_GENERATIVE_OUTPUT_INVALID')
    pixels = np.asarray(patch.pixels)
    changed = np.asarray(patch.changed_mask)
    generated = np.asarray(patch.generated_mask)
    target = np.asarray(target)
    protected = np.asarray(protected)
    if (pixels.dtype != np.float32 or pixels.ndim != 3 or pixels.shape[2] != 3
            or min(pixels.shape[:2]) < 1 or pixels.shape[0] * pixels.shape[1] > 1_048_576
            or any(array.dtype != np.float32 or array.shape != pixels.shape[:2]
                   or not np.isfinite(array).all() for array in
                   (changed, generated, target, protected))
            or not np.isfinite(pixels).all()
            or any(np.any(array < 0) or np.any(array > 1)
                   for array in (changed, generated, target, protected))):
        raise ValueError('GLARE_GENERATIVE_OUTPUT_INVALID')
    x, y = origin
    height, width = source.shape[:2]
    patch_height, patch_width = pixels.shape[:2]
    if x + patch_width > width or y + patch_height > height:
        raise ValueError('GLARE_GENERATIVE_OUTPUT_INVALID')
    original = source[y:y+patch_height, x:x+patch_width]
    actual_changed = np.any(pixels.view(np.uint32) != original.view(np.uint32),
                            axis=2).astype(np.float32)
    forbidden = (target == 0) | (protected > 0)
    if (not np.array_equal(actual_changed, changed)
            or np.any(changed[forbidden] != 0)
            or np.any(generated[forbidden] != 0)
            or np.any((generated != 0) & (generated != 1))):
        raise ValueError('GLARE_GENERATIVE_MASK_INVALID')
    check_active()
    with scratch.reserve(height * width * 20 + 32768) as reservation:
        rgb_path = reservation.path('glare-generated-rgb', '.npy')
        changed_path = reservation.path('glare-generated-changed', '.npy')
        generated_path = reservation.path('glare-generated-mask', '.npy')
        rgb = np.lib.format.open_memmap(rgb_path, mode='w+', dtype='<f4',
                                        shape=source.shape)
        changed_out = np.lib.format.open_memmap(changed_path, mode='w+',
                                                dtype='<f4', shape=(height, width))
        generated_out = np.lib.format.open_memmap(generated_path, mode='w+',
                                                  dtype='<f4', shape=(height, width))
        try:
            for row in range(0, height, 512):
                check_active()
                block = source[row:row+512]
                if not np.isfinite(block).all():
                    raise ValueError('GLARE_GENERATIVE_OUTPUT_INVALID')
                rgb[row:row+512] = block
                changed_out[row:row+512] = 0
                generated_out[row:row+512] = 0
            rgb[y:y+patch_height, x:x+patch_width] = pixels
            changed_out[y:y+patch_height, x:x+patch_width] = changed
            generated_out[y:y+patch_height, x:x+patch_width] = generated
            clipping = 0
            for row in range(0, height, 512):
                check_active()
                block = rgb[row:row+512]
                clipping += int(np.count_nonzero(
                    np.any((block < 0) | (block >= 1), axis=2)))
            rgb.flush(); changed_out.flush(); generated_out.flush()
            reservation.validate()
        finally:
            rgb._mmap.close()
            changed_out._mmap.close()
            generated_out._mmap.close()
        check_active()
        yield GenerativeGlareFiles(rgb_path, changed_path, generated_path,
                                   reservation, clipping / (height * width))
        check_active()

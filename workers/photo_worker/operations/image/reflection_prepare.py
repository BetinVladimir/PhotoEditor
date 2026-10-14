"""V28 reflection pixel preparation; model qualification remains caller-owned."""

from contextlib import contextmanager

import numpy as np

from photo_worker.algorithms.reflection_color import (
    reflection_color_compose, reflection_model_input,
)
from photo_worker.operations.image.reflection_result import ReflectionFiles
from photo_worker.operations.image.reflection_tiles import reflection_proposal


@contextmanager
def prepare_reflection(source, target, protected, strength, profile_id,
                       engine, scratch, *, check_active=lambda: None):
    """Produce closed NPY files in one live reservation for result staging."""
    source = np.asarray(source)
    target = np.asarray(target)
    protected = np.asarray(protected)
    if (source.dtype != np.float32 or source.ndim != 3 or source.shape[2] != 3
            or min(source.shape[:2]) < 1 or source.shape[0] * source.shape[1] > 16_000_000
            or target.dtype != np.float32 or protected.dtype != np.float32
            or target.shape != source.shape[:2] or protected.shape != source.shape[:2]
            or not np.isfinite(target).all() or not np.isfinite(protected).all()
            or not callable(check_active)):
        raise ValueError('REFLECTION_INPUT_INVALID')
    height, width = source.shape[:2]
    check_active()
    model_input, clipped = reflection_model_input(source, profile_id)
    model_output = reflection_proposal(model_input, engine, check_active=check_active)
    del model_input
    check_active()
    with scratch.reserve(height * width * 20 + 16384) as reservation:
        rgb_path = reservation.path('reflection-rgb', '.npy')
        changed_path = reservation.path('reflection-changed', '.npy')
        generated_path = reservation.path('reflection-generated', '.npy')
        rgb = np.lib.format.open_memmap(rgb_path, mode='w+', dtype='<f4',
                                        shape=(height, width, 3))
        changed = np.lib.format.open_memmap(changed_path, mode='w+', dtype='<f4',
                                            shape=(height, width))
        generated = np.lib.format.open_memmap(generated_path, mode='w+', dtype='<f4',
                                              shape=(height, width))
        clipping = 0
        try:
            for y in range(0, height, 512):
                for x in range(0, width, 512):
                    check_active()
                    ys = slice(y, min(y + 512, height))
                    xs = slice(x, min(x + 512, width))
                    original = source[ys, xs]
                    guard = protected[ys, xs]
                    mask = target[ys, xs]
                    result, change, generated_tile = reflection_color_compose(
                        original, model_output[ys, xs], mask, guard, strength,
                        profile_id, clipped[ys, xs])
                    frozen = (mask <= 0) | (guard > 0) | clipped[ys, xs]
                    if not np.array_equal(result[frozen].view(np.uint32),
                                          original[frozen].view(np.uint32)):
                        raise ValueError('REFLECTION_OUTSIDE_MASK_CHANGED')
                    rgb[ys, xs] = result
                    changed[ys, xs] = change.astype(np.float32)
                    generated[ys, xs] = generated_tile.astype(np.float32)
                    clipping += int(np.count_nonzero(np.any((result < 0) | (result >= 1), axis=2)))
            rgb.flush(); changed.flush(); generated.flush()
            reservation.validate()
        finally:
            rgb._mmap.close(); changed._mmap.close(); generated._mmap.close()
        check_active()
        yield ReflectionFiles(rgb_path, changed_path, generated_path,
                              reservation, clipping / (height * width))
        check_active()

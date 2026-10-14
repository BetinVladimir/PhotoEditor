"""V28 donorless glare proposal; source-person/model authority is caller-owned."""

from dataclasses import dataclass

import numpy as np

from photo_worker.algorithms.reflection_color import (
    reflection_color_compose, reflection_model_input,
)


@dataclass(frozen=True)
class GenerativeGlarePatch:
    pixels: np.ndarray
    changed_mask: np.ndarray
    generated_mask: np.ndarray
    clipped_count: int
    requires_review: bool = True


def prepare_generative_glare_patch(source_patch, glasses_mask, protected,
                                    strength, profile_id, engine, *,
                                    check_active=lambda: None):
    """Infer only the effective target and preserve all other source bits."""
    source = np.asarray(source_patch)
    target = np.asarray(glasses_mask)
    guard = np.asarray(protected)
    if (source.dtype != np.float32 or source.ndim != 3 or source.shape[2] != 3
            or min(source.shape[:2]) < 1 or source.shape[0] * source.shape[1] > 1_048_576
            or target.dtype != np.float32 or guard.dtype != np.float32
            or target.shape != source.shape[:2] or guard.shape != target.shape
            or not np.isfinite(target).all() or not np.isfinite(guard).all()
            or np.any(target < 0) or np.any(target > 1)
            or np.any(guard < 0) or np.any(guard > 1)
            or type(strength) not in (int, float) or not 0 <= strength <= 1
            or not callable(check_active)):
        raise ValueError('GLARE_GENERATIVE_PATCH_INVALID')
    check_active()
    model_input, clipped = reflection_model_input(source, profile_id)
    effective = target.copy()
    effective[(guard > 0) | clipped] = 0
    if strength == 0 or not np.any(effective > 0):
        empty = np.zeros(target.shape, np.float32)
        return GenerativeGlarePatch(source.copy(), empty, empty.copy(),
                                    int(np.count_nonzero(clipped)))
    proposal = np.asarray(engine.infer(model_input, effective,
                                       check_active=check_active))
    if (proposal.dtype != np.float32 or proposal.shape != source.shape
            or not np.isfinite(proposal).all() or np.any(proposal < 0)
            or np.any(proposal > 1)):
        raise ValueError('MODEL_OUTPUT_INVALID')
    pixels = source.copy()
    changed = np.zeros(target.shape, np.float32)
    generated = np.zeros(target.shape, np.float32)
    height, width = target.shape
    for y in range(0, height, 512):
        for x in range(0, width, 512):
            check_active()
            ys = slice(y, min(y+512, height))
            xs = slice(x, min(x+512, width))
            result, changed_tile, generated_tile = reflection_color_compose(
                source[ys, xs], proposal[ys, xs], effective[ys, xs],
                guard[ys, xs], strength, profile_id, clipped[ys, xs])
            pixels[ys, xs] = result
            changed[ys, xs] = changed_tile
            generated[ys, xs] = generated_tile
    frozen = (target == 0) | (guard > 0) | clipped
    if (not np.array_equal(pixels[frozen].view(np.uint32),
                           source[frozen].view(np.uint32))
            or np.any(generated[frozen] != 0)
            or not np.isfinite(pixels).all()):
        raise ValueError('GLARE_GENERATIVE_OUTSIDE_MASK_CHANGED')
    check_active()
    return GenerativeGlarePatch(pixels, changed, generated,
                                int(np.count_nonzero(clipped)))

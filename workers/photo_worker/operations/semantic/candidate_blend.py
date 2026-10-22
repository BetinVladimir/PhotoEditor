"""Authority-neutral numerical blend; caller supplies qualified owned pixels.

No confirmation, preview publication or Apply authority is established here.
Partial writes remain caller-private scratch until complete verification.
"""
import numpy as np


def blend_candidate_region(output, original, restored, weight, x0, y0, check_active):
    """Write bounded tiles; zero-weight source pixels keep their exact bits."""
    if (original.dtype != np.float32 or restored.dtype != np.float32
            or weight.dtype != np.float32 or original.shape != restored.shape
            or original.shape[:2] != weight.shape or original.ndim != 3
            or original.shape[2] != 3 or not callable(check_active)):
        raise ValueError("INPAINT_BLEND_INVALID")
    if (not isinstance(output, np.ndarray) or output.dtype != np.float32
            or output.ndim != 3 or output.shape[2] != 3 or not output.flags.writeable
            or type(x0) is not int or type(y0) is not int or x0 < 0 or y0 < 0
            or y0+weight.shape[0] > output.shape[0] or x0+weight.shape[1] > output.shape[1]):
        raise ValueError("INPAINT_BLEND_INVALID")
    height, width = weight.shape
    changed_pixels = 0
    for y in range(0, height, 256):
        for x in range(0, width, 256):
            check_active()
            ys = slice(y, min(y + 256, height))
            xs = slice(x, min(x + 256, width))
            strength = weight[ys, xs]
            if not np.isfinite(strength).all() or np.any((strength < 0) | (strength > 1)):
                raise ValueError("INPAINT_BLEND_INVALID")
            selected = strength > 0
            if not np.any(selected):
                continue
            base = original[ys, xs]
            repair = restored[ys, xs]
            target = output[y0+y:y0+ys.stop, x0+x:x0+xs.stop]
            before = base[selected]
            after = (before + (repair[selected] - before) *
                     strength[selected][:, None])
            changed_pixels += int(np.count_nonzero(np.any(
                after.view(np.uint32) != before.view(np.uint32), axis=1)))
            target[selected] = after
    check_active()
    return changed_pixels


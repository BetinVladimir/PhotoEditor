"""Shared bounded motion-mask morphology for V29 multi-input images."""
import numpy as np


def dilate_motion_disk3(motion, *, check_active=lambda: None):
    if (not isinstance(motion, np.ndarray) or motion.dtype != np.bool_ or motion.ndim != 2
            or not motion.flags.writeable or min(motion.shape) < 1 or motion.size > 200000000):
        raise ValueError('MULTI_MOTION_INVALID')
    height, width = motion.shape
    disk = [(dx, dy) for dy in range(-3, 4) for dx in range(-3, 4) if dx*dx+dy*dy <= 9]
    previous = []
    total = 0
    for y in range(height):
        check_active()
        # Preserve original rows before overwriting; only a 7-row window lives.
        if y == 0:
            previous = [np.zeros(width, bool) for _ in range(3)] + [motion[k].copy() if k < height else np.zeros(width, bool) for k in range(4)]
        else:
            previous.pop(0)
            previous.append(motion[y+3].copy() if y+3 < height else np.zeros(width, bool))
        row = np.zeros(width, bool)
        for dx, dy in disk:
            source = previous[dy+3]
            if dx >= 0:
                row[dx:] |= source[:width-dx] if dx < width else False
            else:
                row[:dx] |= source[-dx:] if -dx < width else False
        motion[y] = row; total += int(np.count_nonzero(row))
    check_active()
    return total/(height*width)

"""V28 reflection preparation stages exact-mask RGB and generated evidence."""

from uuid import uuid4

import numpy as np

from photo_worker.operations.image.reflection_prepare import prepare_reflection
from photo_worker.runtime.scratch import ScratchSpace


class ConstantEngine:
    size = 256

    def infer(self, image, *, check_active):
        check_active()
        assert image.shape == (256, 256, 3)
        return np.full_like(image, .6)


def test_reflection_prepare_preserves_every_outside_and_protected_pixel(tmp_path):
    source = np.full((9, 11, 3), .2, np.float32)
    target = np.zeros((9, 11), np.float32)
    target[2:5, 3:7] = 1
    protected = np.zeros_like(target)
    protected[3, 4] = .001
    with ScratchSpace(tmp_path, uuid4(), uuid4(), 1, 1_000_000,
                      min_free_bytes=0) as scratch:
        with prepare_reflection(source, target, protected, 1,
                                'linear_srgb_v1', ConstantEngine(), scratch) as files:
            rgb = np.load(files.rgb, allow_pickle=False)
            changed = np.load(files.changed, allow_pickle=False)
            generated = np.load(files.generated, allow_pickle=False)
            assert rgb.shape == source.shape
            outside = (target == 0) | (protected > 0)
            assert np.array_equal(rgb[outside].view(np.uint32),
                                  source[outside].view(np.uint32))
            assert not changed[outside].any() and not generated[outside].any()
            assert changed[2, 3] == 1 and generated[2, 3] == 1
            assert files.clipping_fraction == 0

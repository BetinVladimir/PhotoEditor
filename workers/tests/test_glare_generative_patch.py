"""V28 generative glare never changes protected or outside-mask pixels."""

import numpy as np
import pytest

from photo_worker.operations.portrait.glare_generative_patch import prepare_generative_glare_patch


class Engine:
    def infer(self, image, mask, *, check_active):
        check_active()
        assert mask.shape == image.shape[:2]
        return np.full_like(image, .8)


def test_generative_glare_patch_marks_only_effective_pixels():
    source = np.full((32, 48, 3), .2, np.float32)
    target = np.zeros((32, 48), np.float32)
    target[10:20, 10:25] = 1
    protected = np.zeros_like(target)
    protected[12, 12] = .001
    patch = prepare_generative_glare_patch(source, target, protected, .5,
                                            'linear_srgb_v1', Engine())
    frozen = (target == 0) | (protected > 0)
    assert np.array_equal(patch.pixels[frozen].view(np.uint32),
                          source[frozen].view(np.uint32))
    assert not patch.generated_mask[frozen].any()
    assert patch.generated_mask[15, 15] == 1
    assert patch.changed_mask[15, 15] == 1
    assert patch.requires_review is True


def test_generative_glare_patch_refuses_nonfinite_mask():
    source = np.zeros((8, 8, 3), np.float32)
    mask = np.full((8, 8), np.nan, np.float32)
    with pytest.raises(ValueError, match='GLARE_GENERATIVE_PATCH_INVALID'):
        prepare_generative_glare_patch(source, mask,
            np.zeros((8, 8), np.float32), 1, 'linear_srgb_v1', Engine())

"""V28 reflection model color bridge preserves protected/outside RGB bits."""

import numpy as np
import pytest

from photo_worker.algorithms.reflection_color import (
    reflection_color_compose, reflection_model_input,
)


@pytest.mark.parametrize('profile', ['linear_srgb_v1', 'work_rec2020_gamma24_v1'])
def test_reflection_color_bridge_preserves_unselected_pixels(profile):
    source = np.full((2, 3, 3), .3, np.float32)
    model_input, clipped = reflection_model_input(source, profile)
    assert model_input.shape == source.shape and model_input.dtype == np.float32
    target = np.array([[1, 0, 1], [1, 1, 1]], np.float32)
    protected = np.array([[0, 0, .001], [0, 1, 0]], np.float32)
    result, changed, generated = reflection_color_compose(
        source, np.full_like(source, .7), target, protected, .5, profile, clipped)
    for y, x in ((0, 1), (0, 2), (1, 1)):
        assert np.array_equal(result[y, x].view(np.uint32), source[y, x].view(np.uint32))
        assert changed[y, x] == 0 and generated[y, x] == 0
    assert generated[0, 0] == 1 and changed[0, 0] == 1


def test_reflection_color_bridge_rejects_unsupported_profile():
    with pytest.raises(ValueError, match='REFLECTION_COLOR_INPUT_INVALID'):
        reflection_model_input(np.ones((1, 1, 3), np.float32), 'unknown')

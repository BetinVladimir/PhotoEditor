import numpy as np
import pytest
from photo_worker.operations.semantic.candidate_blend import blend_candidate_region


def test_zero_weight_preserves_bits_and_fractional_weight_preserves_hdr():
    original = np.full((2, 2, 3), 2., np.float32)
    restored = np.full_like(original, 4.)
    weight = np.array([[0, .5], [0, 1]], np.float32)
    output = np.full((4, 4, 3), 2., np.float32)
    assert blend_candidate_region(output, original, restored, weight, 1, 1, lambda: None) == 2
    np.testing.assert_array_equal(output[1, 1].view(np.uint32), original[0, 0].view(np.uint32))
    np.testing.assert_array_equal(output[1, 2], [3., 3., 3.])
    np.testing.assert_array_equal(output[2, 2], [4., 4., 4.])
    np.testing.assert_array_equal(output[0], np.full((4, 3), 2., np.float32))


@pytest.mark.parametrize('offset', [(-1, 0), (0, -1), (3, 0), (False, 0)])
def test_rejects_out_of_canvas_or_noninteger_roi(offset):
    rgb = np.ones((2, 2, 3), np.float32); weight = np.ones((2, 2), np.float32)
    with pytest.raises(ValueError, match='INPAINT_BLEND_INVALID'):
        blend_candidate_region(np.zeros((4, 4, 3), np.float32), rgb, rgb, weight, *offset, lambda: None)


@pytest.mark.parametrize('value', [np.nan, -1., 2.])
def test_rejects_invalid_weight_before_writing_that_tile(value):
    rgb = np.ones((2, 2, 3), np.float32); output = np.zeros_like(rgb)
    with pytest.raises(ValueError, match='INPAINT_BLEND_INVALID'):
        blend_candidate_region(output, rgb, rgb, np.full((2, 2), value, np.float32), 0, 0, lambda: None)
    assert not output.any()

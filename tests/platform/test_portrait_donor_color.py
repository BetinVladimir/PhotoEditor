import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor import match_patch_color


def test_color_gains_are_bounded_and_inputs_unchanged():
    source = np.tile([4., .2, 1.1], (64, 1)).astype(np.float32)
    donor = np.ones((64, 3), np.float32)
    result = match_patch_color(source, donor)
    np.testing.assert_allclose(result.gains, [1.25, .8, 1.1], atol=1e-7)
    assert result.warning is None and result.sample_count == 64
    assert not result.gains.flags.writeable
    np.testing.assert_array_equal(donor, np.ones((64, 3), np.float32))


@pytest.mark.parametrize('count', [0, 1, 63])
def test_insufficient_ring_preserves_color_and_requires_review(count):
    result = match_patch_color(np.ones((count, 3), np.float32), np.zeros((count, 3), np.float32))
    np.testing.assert_array_equal(result.gains, [1, 1, 1])
    assert result.warning == 'COLOR_MATCH_UNAVAILABLE'


def test_hdr_median_does_not_overflow_and_black_ring_is_stable():
    hdr = np.full((64, 3), np.finfo(np.float32).max, np.float32)
    np.testing.assert_array_equal(match_patch_color(hdr, hdr).gains, [1, 1, 1])
    black = np.zeros((64, 3), np.float32)
    np.testing.assert_array_equal(match_patch_color(black, black).gains, [1, 1, 1])


def test_bad_ring_samples_fail_instead_of_being_dropped():
    source = np.ones((64, 3), np.float32)
    source[0, 0] = np.nan
    with pytest.raises(ValueError, match='DONOR_COLOR'):
        match_patch_color(source, np.ones((64, 3), np.float32))

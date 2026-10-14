import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor import compose_donor_patch


def test_disabled_pixels_are_bitwise_preserved_and_hdr_not_clipped():
    source = np.random.default_rng(41).uniform(0, 4, (5, 6, 3)).astype(np.float32)
    source[0, 0, 0] = -0.
    donor = np.full_like(source, 8.)
    feather = np.ones((5, 6), np.float32);feather[0] = 0
    protected = np.zeros_like(feather);protected[:, 0] = 1
    original = source.copy()
    result = compose_donor_patch(source, donor, feather, protected, 1, [1, 1, 1])
    disabled = (feather == 0) | (protected == 1)
    np.testing.assert_array_equal(result.view(np.uint32)[disabled], source.view(np.uint32)[disabled])
    np.testing.assert_array_equal(result[~disabled], donor[~disabled])
    np.testing.assert_array_equal(source.view(np.uint32), original.view(np.uint32))


def test_fractional_protection_is_strict_and_unprotected_strength_is_linear():
    source = np.ones((2, 2, 3), np.float32)
    donor = np.full_like(source, 3.)
    result = compose_donor_patch(source, donor, np.full((2, 2), .5, np.float32), np.full((2, 2), .5, np.float32), .5, [1, 1, 1])
    np.testing.assert_array_equal(result.view(np.uint32), source.view(np.uint32))
    unprotected = compose_donor_patch(source, donor, np.full((2, 2), .5, np.float32), np.zeros((2, 2), np.float32), .5, [1, 1, 1])
    np.testing.assert_array_equal(unprotected, np.full_like(source, 1.5))
    zero = compose_donor_patch(source, donor, np.ones((2, 2), np.float32), np.zeros((2, 2), np.float32), 0, [1, 1, 1])
    np.testing.assert_array_equal(zero.view(np.uint32), source.view(np.uint32))


def test_unrepresentable_hdr_and_invalid_gains_fail():
    source = np.ones((2, 2, 3), np.float32)
    donor = np.full_like(source, np.finfo(np.float32).max)
    mask = np.ones((2, 2), np.float32)
    with pytest.raises(ValueError, match='DONOR_PIXELS'):
        compose_donor_patch(source, donor, mask, np.zeros_like(mask), 1, [1.25]*3)
    with pytest.raises(ValueError, match='DONOR_PIXELS'):
        compose_donor_patch(source, source, mask, np.zeros_like(mask), 1, [2]*3)

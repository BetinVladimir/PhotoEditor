import cv2
import numpy as np
import pytest
from photo_worker.algorithms.portrait_donor import donor_mask_geometry


def test_ring_excludes_target_and_any_protected_pixels():
    mask = np.zeros((80, 80), np.float32)
    mask[30:50, 30:50] = 1
    protected = np.zeros_like(mask)
    protected[:, :40] = .1
    geometry = donor_mask_geometry(mask, protected, 100)
    expected_ring = (cv2.GaussianBlur(mask, (0, 0), 1.) > .01) & (mask == 0) & (protected == 0)
    np.testing.assert_array_equal(geometry.color_ring, expected_ring)
    assert geometry.color_ring.any()
    assert not geometry.color_ring[protected > 0].any()
    assert not geometry.color_ring[mask > 0].any()
    expected = mask * cv2.GaussianBlur(mask, (0, 0), 1.)
    np.testing.assert_array_equal(geometry.feather, expected)
    np.testing.assert_array_equal(geometry.feather[mask == 0], 0)
    assert not geometry.feather.flags.writeable


def test_empty_and_fully_protected_rings_do_not_invent_samples():
    empty = np.zeros((12, 12), np.float32)
    geometry = donor_mask_geometry(empty, empty, 100)
    assert not geometry.color_ring.any() and not geometry.feather.any()
    mask = empty.copy();mask[4:8, 4:8] = 1
    assert not donor_mask_geometry(mask, np.ones_like(mask), 100).color_ring.any()


def test_invalid_masks_fail():
    mask = np.zeros((12, 12), np.float32);mask[0, 0] = np.nan
    with pytest.raises(ValueError, match='DONOR_MASK'):
        donor_mask_geometry(mask, np.zeros_like(mask), 100)

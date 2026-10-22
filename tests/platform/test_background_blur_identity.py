"""Unchanged blur regions retain exact encoded source values."""
import numpy as np
from photo_worker.algorithms.background import blur_background_tile


def test_zero_radius_layers_are_exact_identity_with_owned_output():
    rgb = np.random.default_rng(82).random((19, 23, 3), dtype=np.float32)
    subject = np.full((19, 23), .3, np.float32)
    depth = np.random.default_rng(83).random(subject.shape, dtype=np.float32)
    crop = (slice(2, 17), slice(3, 20))
    output = blur_background_tile(rgb, subject, depth, (0,) * 8, crop)
    np.testing.assert_array_equal(output, rgb[crop])
    assert not np.shares_memory(output, rgb)


def test_fully_protected_subject_is_exact_even_with_nonzero_blur():
    rgb = np.random.default_rng(84).random((19, 23, 3), dtype=np.float32)
    subject = np.ones((19, 23), np.float32)
    depth = np.full(subject.shape, .4, np.float32)
    crop = (slice(None), slice(None))
    output = blur_background_tile(rgb, subject, depth, (3,) * 8, crop)
    np.testing.assert_array_equal(output, rgb)


def test_mixed_subject_preserves_exact_hard_foreground():
    rgb = np.random.default_rng(85).random((19, 23, 3), dtype=np.float32)
    subject = np.zeros((19, 23), np.float32)
    subject[:, :8] = 1
    depth = np.full(subject.shape, .4, np.float32)
    output = blur_background_tile(rgb, subject, depth, (3,) * 8, (slice(None), slice(None)))
    np.testing.assert_array_equal(output[:, :8], rgb[:, :8])
    assert np.isfinite(output).all()

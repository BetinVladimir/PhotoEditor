"""Transparent output resize must not mix hidden RGB into visible edges."""
import numpy as np
import pytest
from photo_worker.operations.derivative.resize import _resize_pixels


def test_transparent_hidden_color_does_not_bleed():
    rgb = np.array([[[1., 0., 0.], [0., 0., 1.]]], dtype=np.float32)
    alpha = np.array([[1., 0.]], dtype=np.float32)
    out, opacity = _resize_pixels(rgb, alpha, 1, 1)
    np.testing.assert_allclose(out, [[[1., 0., 0.]]], atol=1e-6)
    np.testing.assert_allclose(opacity, [[.5]], atol=1e-6)
    np.testing.assert_array_equal(rgb[0, 1], [0., 0., 1.])


def test_fully_transparent_resize_is_finite_black():
    rgb = np.ones((2, 2, 3), dtype=np.float32)
    out, alpha = _resize_pixels(rgb, np.zeros((2, 2), dtype=np.float32), 1, 1)
    assert np.isfinite(out).all()
    np.testing.assert_array_equal(out, np.zeros((1, 1, 3)))
    np.testing.assert_array_equal(alpha, np.zeros((1, 1)))


def test_opaque_alpha_preserves_existing_linear_resize():
    rgb = np.array([[[.1, .3, .7], [.8, .5, .2]]], dtype=np.float32)
    opaque, _ = _resize_pixels(rgb, None, 1, 1)
    alpha_out, alpha = _resize_pixels(rgb, np.ones((1, 2), dtype=np.float32), 1, 1)
    np.testing.assert_array_equal(opaque, alpha_out)
    np.testing.assert_array_equal(alpha, [[1.]])


def test_no_resize_owns_buffers_and_invalid_alpha_rejected():
    rgb = np.ones((2, 2, 3), dtype=np.float32)
    alpha = np.ones((2, 2), dtype=np.float32)
    out, opacity = _resize_pixels(rgb, alpha, 2, 2)
    out[:] = 0; opacity[:] = 0
    assert rgb.all() and alpha.all()
    for invalid in (np.ones((1, 2), dtype=np.float32), np.full((2, 2), np.nan, dtype=np.float32), np.full((2, 2), 2., dtype=np.float32)):
        with pytest.raises(ValueError, match='RESIZE_ALPHA_INVALID'):
            _resize_pixels(rgb, invalid, 1, 1)

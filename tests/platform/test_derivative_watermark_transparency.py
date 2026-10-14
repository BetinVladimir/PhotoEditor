import numpy as np
from photo_worker.operations.derivative.watermark import _resize_mark, _over_mark


def test_mark_on_transparent_background_updates_opacity_without_hidden_color():
    rgb = np.array([[[0., 0., 1.]]], dtype=np.float32)
    alpha = np.zeros((1, 1), dtype=np.float32)
    mark = np.array([[[1., 0., 0.]]], dtype=np.float32)
    premult, coverage = _resize_mark(mark, np.ones((1, 1), dtype=np.float32), 1, 1, .5)
    out, opacity, _ = _over_mark(rgb, alpha, premult, coverage, 0, 0)
    np.testing.assert_allclose(out, mark, atol=1e-6)
    np.testing.assert_array_equal(opacity, [[.5]])
    np.testing.assert_array_equal(alpha, [[0.]])
    np.testing.assert_array_equal(rgb, [[[0., 0., 1.]]])


def test_resize_mark_ignores_hidden_rgb():
    mark = np.array([[[1., 0., 0.], [0., 0., 1.]]], dtype=np.float32)
    premult, coverage = _resize_mark(mark, np.array([[1., 0.]], dtype=np.float32), 1, 1, 1.)
    np.testing.assert_array_equal(premult, [[[.5, 0., 0.]]])
    np.testing.assert_array_equal(coverage, [[.5]])


def test_opaque_base_remains_opaque_and_zero_opacity_is_exact_noop():
    rgb = np.full((2, 2, 3), .1234567, dtype=np.float32)
    mark = np.ones((1, 1, 3), dtype=np.float32)
    premult, coverage = _resize_mark(mark, np.ones((1, 1), dtype=np.float32), 1, 1, .5)
    out, alpha, _ = _over_mark(rgb, None, premult, coverage, 0, 0)
    assert alpha is None
    np.testing.assert_array_equal(out[1], rgb[1])
    premult, coverage = _resize_mark(mark, np.ones((1, 1), dtype=np.float32), 1, 1, 0.)
    out, alpha, delta = _over_mark(rgb, None, premult, coverage, 0, 0)
    np.testing.assert_array_equal(out, rgb)
    assert alpha is None and delta == 0

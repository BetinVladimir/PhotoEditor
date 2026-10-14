"""Numeric quality invariants for linear premultiplied watermark compositing."""
import numpy as np
import pytest

from photo_worker.operations.derivative.watermark import _over_mark, _resize_mark


@pytest.mark.parametrize("source_alpha", [None, .25, 0.])
def test_source_over_uses_linear_light_and_combined_alpha(source_alpha):
    rgb = np.full((3, 3, 3), .2, dtype=np.float32)
    alpha = None if source_alpha is None else np.full((3, 3), source_alpha, dtype=np.float32)
    mark = np.full((1, 1, 3), .8, dtype=np.float32)
    mark_alpha = np.full((1, 1), .5, dtype=np.float32)
    before = rgb.copy()
    alpha_before = None if alpha is None else alpha.copy()
    premult, coverage = _resize_mark(mark, mark_alpha, 1, 1, .6)
    out, opacity, maximum = _over_mark(rgb, alpha, premult, coverage, 1, 1)
    mark_coverage = .5 * .6
    base_coverage = 1. if source_alpha is None else source_alpha
    combined = mark_coverage + base_coverage * (1 - mark_coverage)
    linear = (.8**2.4 * mark_coverage + .2**2.4 * base_coverage * (1 - mark_coverage)) / combined
    np.testing.assert_allclose(out[1, 1], linear**(1/2.4), rtol=2e-6, atol=2e-7)
    if source_alpha is None:
        assert opacity is None
    else:
        assert opacity[1, 1] == pytest.approx(combined, abs=2e-7)
        np.testing.assert_array_equal(alpha, alpha_before)
    untouched = np.ones((3, 3), dtype=bool)
    untouched[1, 1] = False
    np.testing.assert_array_equal(out[untouched], before[untouched])
    np.testing.assert_array_equal(rgb, before)
    assert maximum == pytest.approx(abs(linear - .2**2.4), abs=2e-7)
    assert not np.shares_memory(out, rgb)


def test_area_resampling_ignores_hidden_rgb_under_transparent_mark():
    # The hidden blue pixel contributes no premultiplied color after downscale.
    mark = np.array([[[1., 0., 0.], [0., 0., 1.]]], dtype=np.float32)
    alpha = np.array([[1., 0.]], dtype=np.float32)
    premult, coverage = _resize_mark(mark, alpha, 1, 1, 1.)
    np.testing.assert_allclose(premult, [[[.5, 0., 0.]]], atol=1e-7)
    np.testing.assert_allclose(coverage, [[.5]], atol=1e-7)
    source = np.zeros((1, 1, 3), dtype=np.float32)
    source_alpha = np.zeros((1, 1), dtype=np.float32)
    out, opacity, _ = _over_mark(source, source_alpha, premult, coverage, 0, 0)
    np.testing.assert_allclose(out, [[[1., 0., 0.]]], atol=1e-7)
    np.testing.assert_allclose(opacity, [[.5]], atol=1e-7)


def test_zero_coverage_preserves_hidden_source_rgb_bit_exactly():
    source = np.array([[[.123, .456, .789], [.9, .1, .3]]], dtype=np.float32)
    source_alpha = np.array([[0., .2]], dtype=np.float32)
    premult = np.zeros_like(source)
    coverage = np.zeros_like(source_alpha)
    out, opacity, maximum = _over_mark(source, source_alpha, premult, coverage, 0, 0)
    np.testing.assert_array_equal(out, source)
    np.testing.assert_array_equal(opacity, source_alpha)
    assert maximum == 0.
    assert not np.shares_memory(out, source)
    assert not np.shares_memory(opacity, source_alpha)


def test_tiny_positive_coverage_keeps_straight_mark_color_on_transparent_base():
    source = np.array([[[.1, .2, .3]]], dtype=np.float32)
    source_alpha = np.zeros((1, 1), dtype=np.float32)
    mark = np.array([[[.8, .4, .2]]], dtype=np.float32)
    before, before_alpha = source.copy(), source_alpha.copy()
    premult, coverage = _resize_mark(mark, np.ones((1, 1), dtype=np.float32), 1, 1, 1e-7)
    out, opacity, maximum = _over_mark(source, source_alpha, premult, coverage, 0, 0)
    np.testing.assert_allclose(out, mark, rtol=3e-6, atol=3e-7)
    np.testing.assert_allclose(opacity, [[1e-7]], rtol=1e-6)
    assert maximum > 0
    np.testing.assert_array_equal(source, before)
    np.testing.assert_array_equal(source_alpha, before_alpha)

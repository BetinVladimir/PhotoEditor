import numpy as np
import pytest
from photo_worker.algorithms.multi_sampling import sample_inverse_field


def field(height, width):
    y, x = np.mgrid[:height, :width]
    return np.stack([x+.5, y+.5], axis=-1).astype(np.float32)


def collect(rgb, alpha, coordinates, coverage, tile):
    out = np.zeros((*coverage.shape, 3), np.float32); opacity = np.zeros(coverage.shape, np.float32)
    for part in sample_inverse_field(rgb, alpha, coordinates, coverage, tile=tile):
        h, w = part.alpha.shape
        out[part.y:part.y+h, part.x:part.x+w] = part.rgb
        opacity[part.y:part.y+h, part.x:part.x+w] = part.alpha
    return out, opacity


def test_identity_retains_hdr_and_is_tile_invariant():
    rgb = np.arange(105, dtype=np.float32).reshape(5, 7, 3)-10
    alpha = np.ones((5, 7), np.float32); coordinates = field(5, 7); coverage = np.ones((5, 7), bool)
    original = rgb.copy()
    for tile in [1, 3, 7]:
        out, opacity = collect(rgb, alpha, coordinates, coverage, tile)
        np.testing.assert_array_equal(out, rgb); np.testing.assert_array_equal(opacity, alpha)
    np.testing.assert_array_equal(original, rgb)


def test_transparent_neighbour_color_does_not_bleed():
    rgb = np.array([[[4., 2, 1], [1000, 1000, 1000]]], np.float32)
    alpha = np.array([[1., 0]], np.float32)
    result = list(sample_inverse_field(rgb, alpha, np.array([[[1., .5]]], np.float32), np.ones((1, 1), bool)))[0]
    np.testing.assert_array_equal(result.rgb, [[[4, 2, 1]]])
    np.testing.assert_array_equal(result.alpha, [[.5]])


def test_uncovered_and_zero_alpha_are_zero():
    rgb = np.ones((2, 2, 3), np.float32); alpha = np.zeros((2, 2), np.float32)
    out, opacity = collect(rgb, alpha, field(2, 2), np.zeros((2, 2), bool), 1)
    assert not out.any() and not opacity.any()


def test_invalid_original_outside_sampled_area_is_rejected():
    rgb = np.ones((2, 2, 3), np.float32); rgb[1, 1, 0] = np.nan
    with pytest.raises(ValueError, match='MULTI_SAMPLE_INPUT_INVALID'):
        list(sample_inverse_field(rgb, np.ones((2, 2), np.float32), field(1, 1), np.ones((1, 1), bool)))

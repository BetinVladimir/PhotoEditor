import numpy as np
import pytest
from photo_worker.algorithms.focus_pyramid import (
    pyramid_shapes, write_focus_weights, gaussian_downsample, bilinear_upsample,
)


def test_shapes_retain_odd_dimensions_and_specified_level_limit():
    assert pyramid_shapes((65, 99)) == ((65, 99), (33, 50), (17, 25))
    assert pyramid_shapes((1, 7)) == ((1, 7), (1, 4))
    assert len(pyramid_shapes((4096, 4096))) == 7


def test_flat_hdr_weights_equal_epsilon_squared_with_zero_outside_coverage():
    rgb = np.full((11, 13, 3), 4., np.float32); coverage = np.ones((11, 13), bool); coverage[0] = False
    for tile in [1, 4, 13]:
        weight = np.empty((11, 13), np.float64)
        write_focus_weights(rgb, coverage, weight, luma=(.2126, .7152, .0722), tile=tile)
        np.testing.assert_allclose(weight[1:], 1e-12, rtol=1e-12)
        assert not weight[0].any()


def test_focus_weights_have_no_tile_seams():
    yy, xx = np.mgrid[:19, :23]
    rgb = np.repeat(((xx+yy)%2).astype(np.float32)[..., None], 3, axis=2)
    coverage = np.ones((19, 23), bool)
    first = np.empty((19, 23), np.float64); second = np.empty_like(first)
    write_focus_weights(rgb, coverage, first, luma=(.2126, .7152, .0722), tile=3)
    write_focus_weights(rgb, coverage, second, luma=(.2126, .7152, .0722), tile=23)
    np.testing.assert_allclose(first, second, rtol=1e-12, atol=1e-12)


def test_down_up_constant_hdr_and_odd_sizes_are_tile_invariant():
    source = np.full((15, 19, 3), -3., np.float32)
    for tile in [1, 3, 19]:
        coarse = np.empty((8, 10, 3), np.float32); restored = np.empty_like(source)
        gaussian_downsample(source, coarse, tile=tile)
        bilinear_upsample(coarse, restored, tile=tile)
        np.testing.assert_allclose(coarse, -3, atol=1e-6)
        np.testing.assert_allclose(restored, source, atol=1e-6)


def test_alias_and_invalid_weights_rejected():
    value = np.ones((8, 8), np.float64)
    with pytest.raises(ValueError, match='FOCUS_BUFFER_INVALID'):
        gaussian_downsample(value, value)
    with pytest.raises(ValueError, match='FOCUS_WEIGHT_INVALID'):
        write_focus_weights(np.ones((8, 8, 3), np.float32), np.ones((8, 8), bool), value, luma=(1, 1, 1))


def test_normalized_band_blend_and_reference_fallback():
    from photo_worker.algorithms.focus_pyramid import blend_focus_level
    bands = [np.full((7, 9, 3), -2., np.float64), np.full((7, 9, 3), 6., np.float64)]
    weights = [np.ones((7, 9), np.float64), np.full((7, 9), 3., np.float64)]
    weights[0][0, 0] = weights[1][0, 0] = 0
    output = np.empty_like(bands[0])
    blend_focus_level(bands, weights, 0, output, tile=2)
    assert np.all(output[1:] == 4) and np.all(output[0, 1:] == 4)
    np.testing.assert_array_equal(output[0, 0], [-2, -2, -2])


def test_laplacian_reconstruction_preserves_original_hdr_signal():
    from photo_worker.algorithms.focus_pyramid import write_laplacian_band, reconstruct_focus_level
    fine = np.arange(15*19*3, dtype=np.float64).reshape(15, 19, 3)/3-70
    coarse = np.empty((8, 10, 3), np.float64)
    expanded = np.empty_like(fine); band = np.empty_like(fine); result = np.empty_like(fine)
    gaussian_downsample(fine, coarse, tile=3)
    bilinear_upsample(coarse, expanded, tile=2)
    write_laplacian_band(fine, expanded, band, tile=4)
    reconstruct_focus_level(band, expanded, result, tile=5)
    np.testing.assert_allclose(result, fine, rtol=1e-14, atol=1e-12)


def test_focus_level_rejects_negative_weight_and_output_alias():
    from photo_worker.algorithms.focus_pyramid import blend_focus_level
    band = np.ones((5, 5, 3), np.float64); weight = np.ones((5, 5), np.float64)
    with pytest.raises(ValueError, match='FOCUS_BUFFER_INVALID'):
        blend_focus_level([band, band], [weight, weight], 0, band)
    weight[2, 2] = -1
    with pytest.raises(ValueError, match='FOCUS_INPUT_INVALID'):
        blend_focus_level([band, band], [weight, weight], 0, np.empty_like(band))

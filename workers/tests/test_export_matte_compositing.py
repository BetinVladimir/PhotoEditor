"""Export matte uses the target profile's transfer curve, including edges."""

import numpy as np
import pytest

from photo_worker.operations.export.encode import _composite_matte


@pytest.mark.parametrize('icc_id,expected', [
    ('srgb_v1', 0.73535698),
    ('work_rec2020_gamma24_v1', 0.74915354),
])
def test_half_transparent_black_on_white_is_linear_light(icc_id, expected):
    rgb = np.zeros((1, 1, 3), dtype=np.float32)
    alpha = np.full((1, 1), .5, dtype=np.float32)
    result = _composite_matte(rgb, alpha, [1, 1, 1], icc_id)
    np.testing.assert_allclose(result, expected, atol=2e-6)
    assert result.dtype == np.float32


def test_opaque_matte_preserves_original_pixels():
    rgb = np.array([[[.1, .2, .3]]], dtype=np.float32)
    assert _composite_matte(rgb, np.ones((1, 1), dtype=np.float32),
                            [1, 1, 1], 'srgb_v1') is rgb


def test_invalid_alpha_cannot_enter_matte_export():
    with pytest.raises(ValueError, match='MATTE_PIXELS_INVALID'):
        _composite_matte(np.zeros((1, 1, 3), dtype=np.float32),
                         np.array([[np.nan]], dtype=np.float32),
                         [1, 1, 1], 'srgb_v1')

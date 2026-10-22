"""Compositing endpoints preserve the actual encoded source/background."""
import numpy as np
import pytest
from photo_worker.algorithms.background import solid


@pytest.mark.parametrize("asset", [False, True])
def test_solid_exact_endpoints_and_existing_fractional_math(asset):
    rgb = np.random.default_rng(86).random((11, 17, 3), dtype=np.float32)
    background = (np.random.default_rng(87).random(rgb.shape, dtype=np.float32)
                  if asset else (.13, .27, .69))
    alpha = np.full(rgb.shape[:2], .35, np.float32)
    alpha[:, :5] = 1
    alpha[:, 12:] = 0
    output = solid(rgb, alpha, background)
    bg = np.broadcast_to(np.asarray(background, dtype=np.float32), rgb.shape)
    np.testing.assert_array_equal(output[alpha == 1], rgb[alpha == 1])
    np.testing.assert_array_equal(output[alpha == 0], bg[alpha == 0])
    linear = (np.power(rgb, np.float32(2.4), dtype=np.float32) * alpha[..., None]
              + np.power(bg, np.float32(2.4), dtype=np.float32) * (np.float32(1)-alpha[..., None]))
    expected = np.power(np.clip(linear, 0, 1), np.float32(1/2.4), dtype=np.float32)
    np.testing.assert_array_equal(output[(alpha > 0) & (alpha < 1)],
                                  expected[(alpha > 0) & (alpha < 1)])
    assert not np.shares_memory(output, rgb)

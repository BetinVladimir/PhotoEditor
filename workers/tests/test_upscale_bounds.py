"""Upscale RAM admission includes its full-frame alpha interpolation."""

import pytest

from photo_worker.operations.image.upscale import _required_upscale_ram


def test_upscale_alpha_needs_more_ram_than_rgb_only():
    plain = _required_upscale_ram(3000, 4000, 4, False)
    alpha = _required_upscale_ram(3000, 4000, 4, True)
    assert plain < 1024**3
    assert alpha - plain == 3000 * 4000 * 16 * 8
    assert alpha > 2 * 1024**3


@pytest.mark.parametrize('height,width,scale,alpha', [
    (0, 1, 2, False), (1, 0, 2, False), (1, 1, 0, False),
    (True, 1, 2, False), (1, 1, 2, 1),
])
def test_upscale_rejects_invalid_ram_inputs(height, width, scale, alpha):
    with pytest.raises(ValueError, match='UPSCALE_SIZE_INVALID'):
        _required_upscale_ram(height, width, scale, alpha)

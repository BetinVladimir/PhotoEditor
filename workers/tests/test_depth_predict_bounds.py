"""Depth prediction admits its full-frame color and depth allocations."""

import pytest

from photo_worker.operations.depth.predict import _required_depth_ram


def test_large_depth_frame_exceeds_small_ram_profile():
    assert _required_depth_ram(10_000, 10_000, False) > 10 * 1024**3
    assert _required_depth_ram(1024, 1024, True) < 1024**3
    assert _required_depth_ram(3000, 4000, True) > _required_depth_ram(3000, 4000, False)


@pytest.mark.parametrize('height,width,alpha', [(0, 1, False), (1, 0, False), (True, 4, False), (1, 1, 1)])
def test_depth_ram_rejects_invalid_dimensions(height, width, alpha):
    with pytest.raises(ValueError, match='DEPTH_SIZE_INVALID'):
        _required_depth_ram(height, width, alpha)

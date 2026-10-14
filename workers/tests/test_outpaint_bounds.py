"""Outpaint reserves its full-frame working set before model and input IO."""

import pytest

from photo_worker.operations.image.outpaint import _required_outpaint_ram


def test_outpaint_large_canvas_exceeds_small_ram_profile():
    assert _required_outpaint_ram(10_000, 10_000) > 14 * 1024**3
    assert _required_outpaint_ram(1024, 1024) < 2 * 1024**3


@pytest.mark.parametrize('height,width', [(0, 1), (1, 0), (-1, 4), (True, 4)])
def test_outpaint_rejects_invalid_canvas_size(height, width):
    with pytest.raises(ValueError, match='OUTPAINT_SIZE_INVALID'):
        _required_outpaint_ram(height, width)

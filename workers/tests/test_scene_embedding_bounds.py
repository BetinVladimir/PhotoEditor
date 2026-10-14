"""Scene embedding admits the full-frame color conversion before pixel IO."""

import pytest

from photo_worker.operations.features.scene import _required_embedding_ram


def test_large_embedding_frame_exceeds_small_ram_profile():
    assert _required_embedding_ram(10_000, 10_000) > 8 * 1024**3
    assert _required_embedding_ram(1024, 1024) < 1024**3


@pytest.mark.parametrize('height,width', [(0, 1), (1, 0), (-1, 4), (True, 4)])
def test_embedding_rejects_invalid_dimensions(height, width):
    with pytest.raises(ValueError, match='SCENE_SIZE_INVALID'):
        _required_embedding_ram(height, width)

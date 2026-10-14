"""Face19 parsing admits full-frame color and per-face warp maps."""

import pytest

from photo_worker.operations.face.parse import _required_face_parse_ram


def test_large_face_parse_exceeds_small_ram_profile():
    assert _required_face_parse_ram(10_000, 10_000) > 18 * 1024**3
    assert _required_face_parse_ram(1024, 1024) < 1024**3


@pytest.mark.parametrize('height,width', [(0, 1), (1, 0), (-1, 4), (True, 4)])
def test_face_parse_rejects_invalid_dimensions(height, width):
    with pytest.raises(ValueError, match='FACE_PARSE_SIZE_INVALID'):
        _required_face_parse_ram(height, width)

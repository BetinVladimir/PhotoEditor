"""Face19 skin conversion reserves its full-frame probability and PNG buffers."""

import pytest

from photo_worker.operations.mask.face_skin import _required_face_skin_ram


def test_large_face_skin_exceeds_small_ram_profile():
    assert _required_face_skin_ram(10_000, 10_000) > 6 * 1024**3
    assert _required_face_skin_ram(1024, 1024) < 512 * 1024**2


@pytest.mark.parametrize('height,width', [(0, 1), (1, 0), (-1, 4), (True, 4)])
def test_face_skin_rejects_invalid_dimensions(height, width):
    with pytest.raises(ValueError, match='FACE_SKIN_SIZE_INVALID'):
        _required_face_skin_ram(height, width)

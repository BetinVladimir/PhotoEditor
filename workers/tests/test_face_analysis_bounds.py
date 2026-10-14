"""SFD and FAN full-frame preparation share an admitted RAM floor."""

import pytest

from photo_worker.operations.face.resources import required_face_analysis_ram


def test_large_face_analysis_exceeds_small_ram_profile():
    assert required_face_analysis_ram(10_000, 10_000, False) > 14 * 1024**3
    assert required_face_analysis_ram(1024, 1024, True) < 1024**3
    assert required_face_analysis_ram(3000, 4000, True) > required_face_analysis_ram(3000, 4000, False)


@pytest.mark.parametrize('height,width,alpha', [(0, 1, False), (1, 0, False), (True, 4, False), (1, 1, 1)])
def test_face_analysis_rejects_invalid_dimensions(height, width, alpha):
    with pytest.raises(ValueError, match='FACE_ANALYSIS_SIZE_INVALID'):
        required_face_analysis_ram(height, width, alpha)

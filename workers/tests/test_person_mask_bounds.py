"""Person mask admission includes its full-frame guided refinement buffers."""

import pytest

from photo_worker.operations.mask.person import _required_person_ram


def test_large_person_mask_exceeds_small_ram_profile():
    assert _required_person_ram(10_000, 10_000, False) > 14 * 1024**3
    assert _required_person_ram(1024, 1024, True) < 1024**3
    assert _required_person_ram(3000, 4000, True) > _required_person_ram(3000, 4000, False)


@pytest.mark.parametrize('height,width,alpha', [(0, 1, False), (1, 0, False), (True, 4, False), (1, 1, 1)])
def test_person_ram_rejects_invalid_dimensions(height, width, alpha):
    with pytest.raises(ValueError, match='PERSON_SIZE_INVALID'):
        _required_person_ram(height, width, alpha)

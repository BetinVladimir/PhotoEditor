"""Prompt masks admit whole-frame buffers before model and artifact IO."""

import pytest

from photo_worker.operations.mask.prompt import _required_prompt_ram


def test_large_prompt_mask_exceeds_small_ram_profile():
    assert _required_prompt_ram(10_000, 10_000) > 10 * 1024**3
    assert _required_prompt_ram(1024, 1024) < 1024**3


@pytest.mark.parametrize('height,width', [(0, 1), (1, 0), (-1, 4), (True, 4)])
def test_prompt_ram_rejects_invalid_dimensions(height, width):
    with pytest.raises(ValueError, match='PROMPT_SIZE_INVALID'):
        _required_prompt_ram(height, width)

"""Generation ROI boundaries stay bounded before model and output allocation."""

import numpy as np
import pytest

from photo_worker.operations.image.generate import _square_roi
from photo_worker.operations.image.inpaint import _required_frame_ram, _roi_pixel_budget


def test_generation_roi_does_not_materialize_pixel_coordinates(monkeypatch):
    mask = np.zeros((20, 30), bool)
    mask[7, 10] = True
    monkeypatch.setattr(np, 'nonzero', lambda *_: pytest.fail('per-pixel coordinates'))
    assert _square_roi(mask, 2) == (8, 5, 13, 10)


def test_generation_roi_clips_square_to_canvas():
    mask = np.zeros((12, 24), bool)
    mask[0, 0] = True
    mask[11, 23] = True
    assert _square_roi(mask, 64) == (0, 0, 24, 12)


def test_generation_empty_mask_preserves_error_code():
    with pytest.raises(ValueError, match='^GENERATION_MASK_EMPTY$'):
        _square_roi(np.zeros((8, 8), bool), 0)


def test_generation_budget_reserves_full_frame_before_roi():
    height = width = 10_000
    ram = 2 * 1024**3
    assert ram < _required_frame_ram(height, width)
    assert _roi_pixel_budget(ram, height, width) == 0
    ram = 4 * 1024**3
    assert 0 < _roi_pixel_budget(ram, height, width) < height * width

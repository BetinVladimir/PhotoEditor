"""Alpha matting finds its ROI without per-pixel coordinate arrays."""

import numpy as np
import pytest

from photo_worker.operations.alpha.refine import _boundary_roi, _required_refine_ram


def test_refine_boundary_roi_uses_axis_projections(monkeypatch):
    boundary=np.zeros((20,30),bool);boundary[3,4]=True;boundary[15,21]=True
    monkeypatch.setattr(np,'where',lambda *_: pytest.fail('per-pixel coordinates'))
    assert _boundary_roi(boundary,2)==(2,1,24,18)
    assert _boundary_roi(np.zeros((20,30),bool),2) is None


def test_large_refine_frame_exceeds_small_ram_profile():
    assert _required_refine_ram(10_000,10_000)>14*1024**3
    assert _required_refine_ram(1024,1024)<1024**3


@pytest.mark.parametrize('height,width',[(0,1),(1,0),(True,4)])
def test_refine_rejects_invalid_dimensions(height,width):
    with pytest.raises(ValueError,match='MATTING_SIZE_INVALID'):
        _required_refine_ram(height,width)

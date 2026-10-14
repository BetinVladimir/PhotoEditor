"""Native detail exact source admission on wide RGB/alpha canvases."""
import numpy as np
import pytest
from photo_worker.v2.render_detail import _same_detail_pixels


@pytest.mark.parametrize('channels',[None,3])
def test_detail_comparison_is_tiled_and_bit_exact(monkeypatch,channels):
    shape=(2,4099) if channels is None else (2,4099,channels)
    expected=np.zeros(shape,np.float32);actual=expected.copy()
    observed=[];original=np.array_equal
    def equal(left,right):
        assert left.shape[0]<=256 and left.shape[1]<=256
        observed.append(left.shape)
        return original(left,right)
    monkeypatch.setattr(np,'array_equal',equal)
    _same_detail_pixels(actual,expected,lambda:None)
    assert len(observed)==17 and observed[-1][1]==3
    actual[1,-1]=-0.0
    with pytest.raises(ValueError,match='CURRENT_PIXELS_CHANGED'):
        _same_detail_pixels(actual,expected,lambda:None)


def test_detail_comparison_checks_cancel_between_tiles():
    expected=np.zeros((1,1024,3),np.float32);calls=[]
    def active():
        calls.append(None)
        if len(calls)==2:raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):
        _same_detail_pixels(expected.copy(),expected,active)
    assert len(calls)==2

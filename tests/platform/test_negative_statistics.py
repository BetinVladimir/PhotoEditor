import numpy as np
import pytest
from photo_worker.operations.optical.negative_statistics import negative_clipping_fraction


def test_negative_clipping_counts_pixels_at_full_resolution_and_preserves_hdr():
    rgb=np.zeros((513,3,3),np.float32)
    rgb[0,0]=[1,2,3];rgb[512,2]=[-.1,.5,.5];original=rgb.copy()
    assert negative_clipping_fraction(rgb,check_active=lambda:None)==2/(513*3)
    np.testing.assert_array_equal(rgb,original)
    rgb[0,0]=.999
    assert negative_clipping_fraction(rgb,check_active=lambda:None)==1/(513*3)


def test_negative_clipping_refuses_nonfinite_or_cancellation():
    rgb=np.ones((1,1,3),np.float32);rgb[0,0,0]=np.nan
    with pytest.raises(ValueError,match='NEGATIVE_STATISTICS_INVALID'):
        negative_clipping_fraction(rgb,check_active=lambda:None)
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        negative_clipping_fraction(np.ones((1,1,3),np.float32),check_active=cancelled)

"""Sky replacement respects exact protected-pixel exclusions."""
import numpy as np
import pytest
from photo_worker.algorithms.background import replace_sky_tile


def test_sky_preserves_fractional_protected_edges_and_reports_zero_coverage():
    rgb=np.random.default_rng(156).random((13,17,3),dtype=np.float32)
    asset=np.ones_like(rgb);sky=np.ones((13,17),np.float32)
    protected=np.zeros((13,17),np.float32);protected[:,:4]=1/65535
    protected[:,4:8]=.2;protected[:,8:12]=1
    output,effective=replace_sky_tile(rgb,asset,sky,protected,np.ones(3,np.float32))
    np.testing.assert_array_equal(output[protected>0],rgb[protected>0])
    assert np.all(effective[protected>0]==0)
    assert np.all(effective[protected==0]==1)
    np.testing.assert_array_equal(output[protected==0],asset[protected==0])


@pytest.mark.parametrize('bad',[float('nan'),float('inf'),float('-inf')])
def test_sky_refuses_nonfinite_gains_before_compositing(bad):
    rgb=np.full((4,4,3),.4,np.float32);mask=np.ones((4,4),np.float32)
    with pytest.raises(ValueError,match='SKY_INPUT_INVALID'):
        replace_sky_tile(rgb,rgb,mask,np.zeros_like(mask),np.array([1,bad,1],np.float32))


@pytest.mark.parametrize("weight", [float("nan"), float("inf"), float("-inf")])
def test_sky_statistics_refuse_nonfinite_weight(weight):
    from photo_worker.algorithms.background import sky_gains
    with pytest.raises(ValueError, match="SKY_STATS_INVALID"):
        sky_gains(np.ones(3, np.float64), np.ones(3, np.float64), weight, .25)

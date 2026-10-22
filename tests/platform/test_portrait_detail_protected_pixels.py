"""Protected probability edges must be exact exclusions in portrait detail."""
import numpy as np
import pytest
from photo_worker.algorithms.portrait_detail import apply_region


@pytest.mark.parametrize('kind',['teeth','sclera','shine','under_eye','cloth'])
def test_detail_preserves_every_nonzero_protected_pixel(kind):
    rgb=np.random.default_rng(155).uniform(.1,.8,(13,17,3)).astype(np.float32)
    mask=np.ones((13,17),np.float32)
    protected=np.zeros((13,17),np.float32)
    protected[:,:4]=1/65535
    protected[:,4:8]=.1
    protected[:,8:12]=1
    output,changed,_,_,reason=apply_region(rgb,mask,kind,.7,protected)
    np.testing.assert_array_equal(output[protected>0],rgb[protected>0])
    assert reason is None and changed==13*5
    assert np.isfinite(output).all()


def test_fractional_protection_over_whole_image_is_empty_effect():
    rgb=np.full((13,17,3),.4,np.float32)
    output,changed,_,_,reason=apply_region(rgb,np.ones((13,17),np.float32),
        'teeth',.7,np.full((13,17),1e-6,np.float32))
    np.testing.assert_array_equal(output,rgb)
    assert changed==0 and reason=='empty_mask'
    assert not np.shares_memory(output,rgb)

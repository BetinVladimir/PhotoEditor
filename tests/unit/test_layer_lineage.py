import numpy as np
import pytest
from photo_worker.algorithms.layer_lineage import generated_coverage


def coverage(mask,**kwargs):return generated_coverage(np.array([mask],np.float32),opacity=kwargs.pop('opacity',1),blend=kwargs.pop('blend','normal'),max_tile_bytes=4096,**kwargs)


def test_normal_replacement_partial_and_protected_inheritance():
    s=np.ones((1,4),np.float32);p=np.array([[0,1,0,1]],np.float32)
    protected=np.array([[0,0,0,.001]],np.float32)
    result=coverage([0,.5,1,1],source=s,proposal=p,protected=protected)
    np.testing.assert_array_equal(result,[[1,1,0,1]])
    np.testing.assert_array_equal(s,1)

@pytest.mark.parametrize('blend',['multiply','screen'])
def test_blends_retain_source_dependency(blend):
    result=coverage([1,0],blend=blend,source=np.array([[1,0]],np.float32),proposal=np.array([[0,1]],np.float32))
    np.testing.assert_array_equal(result,[[1,0]])


def test_zero_opacity_missing_parent_and_binary_validation():
    s=np.array([[1,0]],np.float32);p=np.ones_like(s)
    np.testing.assert_array_equal(coverage([1,1],opacity=0,source=s,proposal=p),s)
    assert coverage([1,1]) is None
    with pytest.raises(ValueError,match='BINARY'):coverage([1,1],source=np.full((1,2),.5,np.float32))
    with pytest.raises(ValueError):generated_coverage(p,opacity=1,blend='normal',max_tile_bytes=1,source=s)


def test_cancel_before_any_processing():
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):coverage([1],check_active=cancel)

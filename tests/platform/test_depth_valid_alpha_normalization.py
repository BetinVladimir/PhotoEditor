"""Depth minmax_valid boundary; authored, not executed during coding."""
import numpy as np
import pytest
from photo_worker.operations.depth.prepare import normalize_depth
from photo_worker.runtime.arrays import read_alpha_array

@pytest.mark.parametrize('dtype',['uint16','float32'])
def test_both_supported_alpha_encodings_exclude_transparent_depth(tmp_path,dtype):
    alpha=np.array([[0,1],[1,1]],dtype=dtype)
    path=tmp_path/'alpha.npy';np.save(path,alpha,allow_pickle=False)
    with read_alpha_array(path,(2,2),lambda:None) as mapped:
        missing=mapped==0
    depth,flat=normalize_depth(np.array([[100,2],[4,6]],np.float32),2,2,invalid_mask=missing)
    np.testing.assert_array_equal(depth,np.array([[0,0],[.5,1]],np.float32))
    assert flat is False

def test_transparent_extremes_do_not_hide_flat_valid_depth():
    depth,flat=normalize_depth(np.array([[100,3],[3,3]],np.float32),2,2,invalid_mask=np.array([[True,False],[False,False]]))
    np.testing.assert_array_equal(depth,np.zeros((2,2),np.float32));assert flat is True

def test_fully_transparent_depth_is_invalid_flat_zero():
    depth,flat=normalize_depth(np.array([[1,2],[3,4]],np.float32),2,2,invalid_mask=np.ones((2,2),np.bool_))
    np.testing.assert_array_equal(depth,np.zeros((2,2),np.float32));assert flat is True

@pytest.mark.parametrize('mask',[np.zeros((2,2),np.float32),np.zeros((1,2),np.bool_),[[False,False],[False,False]]])
def test_invalid_mask_layout_is_rejected(mask):
    with pytest.raises(ValueError,match='DEPTH_INVALID_MASK'):
        normalize_depth(np.array([[1,2],[3,4]],np.float32),2,2,invalid_mask=mask)

def test_missing_alpha_keeps_existing_depth_normalization():
    raw=np.array([[1,2],[3,4]],np.float32)
    depth,flat=normalize_depth(raw,2,2)
    np.testing.assert_allclose(depth,(raw-1)/3);assert flat is False

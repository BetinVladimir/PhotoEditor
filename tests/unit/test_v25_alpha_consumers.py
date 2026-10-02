import numpy as np
import pytest
from photo_worker.runtime.arrays import read_alpha_array
from photo_worker.operations.identity.adapter import _changed_pixels


def test_strict_alpha_reader_accepts_only_two_valid_encodings(tmp_path):
    path=tmp_path/'alpha.npy'
    for values in (np.array([[0,32768,65535]],np.uint16),np.array([[0,.5,1]],np.float32)):
        np.save(path,values,allow_pickle=False)
        with read_alpha_array(path,(1,3),lambda:None) as mapped:
            assert mapped.dtype==values.dtype
            np.testing.assert_array_equal(mapped,values)
    for values in (np.ones((1,3),np.float64),np.array([[0,2,1]],np.float32)):
        np.save(path,values,allow_pickle=False)
        with pytest.raises(ValueError):
            with read_alpha_array(path,(1,3),lambda:None):pass


def test_identity_changed_pixels_compares_mixed_alpha_units(tmp_path):
    rgb=np.ones((1,3,3),np.float32)
    legacy=np.array([[0,32768,65535]],np.uint16)
    opacity=legacy.astype(np.float32)/np.float32(65535)
    target=np.lib.format.open_memmap(tmp_path/'changed.npy',mode='w+',dtype='<f4',shape=(1,3))
    try:
        _changed_pixels(rgb,rgb,legacy,opacity,target,lambda:None)
        np.testing.assert_array_equal(target,0)
        opacity[0,1]=.1
        _changed_pixels(rgb,rgb,legacy,opacity,target,lambda:None)
        np.testing.assert_array_equal(target,[[0,1,0]])
        _changed_pixels(rgb,rgb,None,np.ones((1,3),np.float32),target,lambda:None)
        np.testing.assert_array_equal(target,0)
    finally:target._mmap.close()


def test_statistics_coverage_matches_legacy_alpha_and_rejects_nan():
    from photo_worker.algorithms.statistics import coverage_counts_bounded
    rgb=np.array([[[.1,.2,.3],[.4,.5,.6],[.7,.8,.9]]],np.float32)
    legacy=np.array([[0,32768,65535]],np.uint16)
    opacity=legacy.astype(np.float32)/np.float32(65535)
    np.testing.assert_array_equal(coverage_counts_bounded(rgb,alpha=legacy),coverage_counts_bounded(rgb,alpha=opacity))
    opacity[0,0]=np.nan
    with pytest.raises(ValueError,match='ALPHA_INVALID'):coverage_counts_bounded(rgb,alpha=opacity)

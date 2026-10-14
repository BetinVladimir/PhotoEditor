import numpy as np
import pytest
from photo_worker.operations.optical.negative_changes import write_negative_changed_mask


def test_changed_mask_tracks_bit_exact_rgb_including_edge_tile_and_signed_zero(tmp_path):
    source=np.zeros((513,3,3),np.float32);output=source.copy()
    output[0,1,0]=-0.;output[512,2,1]=0.1
    changed=np.lib.format.open_memmap(tmp_path/'changed.npy',mode='w+',dtype=np.float32,shape=source.shape[:2])
    count=write_negative_changed_mask(source,output,changed,check_active=lambda:None,ram_max_bytes=1<<30)
    expected=np.zeros_like(changed);expected[0,1]=1;expected[512,2]=1
    assert count==2
    np.testing.assert_array_equal(changed,expected)


@pytest.mark.parametrize('mode',['alias','nonfinite','ram','cancel'])
def test_changed_mask_failures_do_not_write_first_tile(mode):
    source=np.zeros((2,2,3),np.float32);output=source.copy();changed=np.full((2,2),-1,np.float32)
    if mode=='alias':changed=source[:,:,0]
    if mode=='nonfinite':output[0,0,0]=np.nan
    before=changed.copy()
    def active():
        if mode=='cancel':raise ValueError('cancel')
    with pytest.raises(ValueError):write_negative_changed_mask(source,output,changed,
        check_active=active,ram_max_bytes=1 if mode=='ram' else 1<<30)
    np.testing.assert_array_equal(changed,before)

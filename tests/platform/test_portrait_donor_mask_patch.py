import numpy as np
import pytest
from photo_worker.v2.portrait_donor_mask_patch import select_donor_mask_patch,plan_donor_mask_patch
from photo_worker.algorithms.portrait_donor import donor_mask_geometry


@pytest.mark.parametrize('edge',[False,True])
def test_planned_halo_preserves_full_canvas_feather_and_color_ring(edge):
    target=np.zeros((700,900),np.float32);guard=np.zeros_like(target)
    y,x=(0,0) if edge else (350,450)
    target[y:y+20,x:x+30]=.7
    origin,size=plan_donor_mask_patch(target,250,check_active=lambda:None)
    patch,protected=select_donor_mask_patch(target,guard,origin,size,check_active=lambda:None)
    full=donor_mask_geometry(target,guard,250);small=donor_mask_geometry(patch,protected,250)
    px,py=origin;w,h=size
    np.testing.assert_allclose(small.feather,full.feather[py:py+h,px:px+w],rtol=1e-6,atol=1e-7)
    np.testing.assert_array_equal(small.color_ring,full.color_ring[py:py+h,px:px+w])
    ring=np.zeros_like(full.color_ring);ring[py:py+h,px:px+w]=small.color_ring
    np.testing.assert_array_equal(ring,full.color_ring)


def test_multi_tile_scan_returns_owned_patch_and_preserves_inputs():
    target=np.zeros((900,1100),np.float32);target[450:470,700:720]=1
    guard=np.zeros_like(target);target[10,10]=1;guard[10,10]=1e-8
    original=target.copy();calls=[]
    patch,protected=select_donor_mask_patch(target,guard,(690,440),(40,40),check_active=lambda:calls.append(1))
    assert len(calls)==7 and patch.shape==(40,40)
    assert not patch.flags.writeable and not protected.flags.writeable
    assert not np.shares_memory(patch,target)
    np.testing.assert_array_equal(target,original)
    np.testing.assert_array_equal(patch[10:30,10:30],1)


def test_effective_target_outside_patch_rejected():
    target=np.zeros((900,1100),np.float32);target[800,1000]=1
    with pytest.raises(ValueError,match='INCOMPLETE'):
        select_donor_mask_patch(target,np.zeros_like(target),(0,0),(40,40),check_active=lambda:None)


def test_cancelled_scan_returns_no_patch():
    def cancel():raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):
        select_donor_mask_patch(np.zeros((100,100),np.float32),np.zeros((100,100),np.float32),
                                (0,0),(40,40),check_active=cancel)

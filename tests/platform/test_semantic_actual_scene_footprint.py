"""Actual mask sampler must cover pixels beyond transformed proxy centers."""
import numpy as np
import pytest
from photo_worker.algorithms.semantic_scene_projection import verify_actual_scene_footprint
from photo_worker.algorithms.semantic_transfer import iter_planar_mask_warp_tiles


def inputs():
    mask=np.zeros((256,256),np.float32);mask[23,23]=1
    matrix=np.diag([4.,4.,1.])
    labels=np.ones((256,256),np.uint16);guard=np.zeros((256,256),np.float32)
    return mask,matrix,labels,guard,np.zeros((32,32),bool)


def test_magnified_support_marks_actual_bins_beyond_proxy_center():
    mask,matrix,labels,guard,domain=inputs()
    verified=verify_actual_scene_footprint(mask,matrix,labels,guard,{1},domain,32)
    assert verified[11,11]
    assert not verified[0,0]
    output=np.zeros((256,256),np.float32)
    for part,tile in iter_planar_mask_warp_tiles(mask,matrix,output.shape):output[part]=tile
    assert output[92,92]==1


@pytest.mark.parametrize('protected',[False,True])
def test_actual_magnified_pixel_cannot_cross_foreground_or_fractional_protection(protected):
    mask,matrix,labels,guard,domain=inputs()
    if protected:guard[92,92]=np.nextafter(np.float32(0),np.float32(1))
    else:labels[92,92]=12
    with pytest.raises(ValueError,match='PROTECTED' if protected else 'FOREGROUND'):
        verify_actual_scene_footprint(mask,matrix,labels,guard,{1},domain,32)

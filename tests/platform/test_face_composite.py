from uuid import UUID
import numpy as np
import pytest
from photo_worker.algorithms.face_composite import FaceCorrection,composite_faces
from photo_worker.contracts.images import Rect


def collect(source,patches,tile=256):
    output=np.empty_like(source)
    for part in composite_faces(source,patches,tile_size=tile):
        h,w=part.rgb.shape[:2];output[part.y:part.y+h,part.x:part.x+w]=part.rgb
        assert not part.rgb.flags.writeable
    return output

def patch(i,x,y,w,h,amount,weight=1.):
    return FaceCorrection(UUID(int=i),Rect(x=x,y=y,width=w,height=h),np.full((h,w,3),amount,np.float32),np.full((h,w),weight,np.float32))

def test_overlap_normalized_and_order_independent():
    source=np.full((350,700,3),.2,np.float32)
    a=patch(2,50,50,400,200,.2);b=patch(1,250,100,400,200,.4)
    expected=collect(source,[a,b])
    assert np.allclose(expected[150,300],.5)
    assert np.allclose(expected[75,100],.4)
    assert np.array_equal(expected[:50],source[:50])
    for tile in (256,512,1024):assert np.array_equal(collect(source,[b,a],tile),expected)

def test_soft_weight_is_not_applied_twice():
    source=np.full((20,20,3),.2,np.float32)
    p=patch(1,0,0,20,20,.1,.5)
    assert np.allclose(collect(source,[p]),.3)
    p.weight[5:10]=0;p.delta[5:10]=0
    assert np.array_equal(collect(source,[p])[5:10],source[5:10])

def test_invalid_patch_and_cancel():
    source=np.zeros((20,20,3),np.float32);p=patch(1,0,0,20,20,.1)
    with pytest.raises(ValueError):collect(source,[p,p])
    p.weight[0,0]=0
    with pytest.raises(ValueError):collect(source,[p])
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):next(composite_faces(source,[],check_active=cancel))

import numpy as np
import pytest
from photo_worker.algorithms.warp import warp_pair_tiles
from photo_worker.algorithms.registration import PairAlignment


def alignment(matrix):return PairAlignment(np.array(matrix,np.float64),1.,None,0,'user_confirmed',False,())

def assemble(image,transform,shape,tile):
    output=np.empty((*shape,3),np.float32);valid=np.empty(shape,bool)
    for part in warp_pair_tiles(image,transform,shape,tile=tile):
        h,w=part.valid.shape
        output[part.y:part.y+h,part.x:part.x+w]=part.rgb
        valid[part.y:part.y+h,part.x:part.x+w]=part.valid
    return output,valid


def test_identity_and_eroded_edges():
    image=np.random.default_rng(7).random((269,531,3),dtype=np.float32)
    output,valid=assemble(image,alignment(np.eye(3)),image.shape[:2],256)
    assert np.max(np.abs(output-image))<2/65535
    assert valid[1:-1,1:-1].all() and not valid[0].any() and not valid[-1].any()
    assert not valid[:,0].any() and not valid[:,-1].any()


def test_half_pixel_interpolates_linear_light():
    image=np.zeros((12,16,3),np.float32);image[:,1::2]=1
    output,valid=assemble(image,alignment([[1,0,.5],[0,1,0],[0,0,1]]),(12,16),256)
    expected=np.float32(.5**(1/2.4))
    assert np.max(np.abs(output[valid]-expected))<2/65535
    assert float(output[5,5,0])>.7


def test_rotated_scaled_tiles_have_identical_pixels_and_validity():
    import cv2
    image=np.random.default_rng(4).random((623,831,3),dtype=np.float32)
    matrix=np.vstack([cv2.getRotationMatrix2D((415,311),3.2,.96),[0,0,1]])
    matrix[:2,2]+=[.37,-.62]
    transform=alignment(matrix)
    one,valid=assemble(image,transform,(637,839),256)
    for tile in (512,1024):
        other,other_valid=assemble(image,transform,(637,839),tile)
        assert np.array_equal(one,other) and np.array_equal(valid,other_valid)


def test_large_translation_zero_border_and_cancellation():
    image=np.ones((16,16,3),np.float32)
    output,valid=assemble(image,alignment([[1,0,1e12],[0,1,-1e12],[0,0,1]]),(16,16),256)
    assert not output.any() and not valid.any()
    calls=0
    def check():
        nonlocal calls
        calls+=1
        if calls==5:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        list(warp_pair_tiles(image,alignment(np.eye(3)),(16,16),check_active=check))


def test_output_limit_precedes_allocation():
    with pytest.raises(ValueError,match='SHAPE'):
        list(warp_pair_tiles(np.ones((8,8,3),np.float32),alignment(np.eye(3)),(20001,20001)))


def test_incremental_tiles_do_not_allocate_a_linear_frame():
    import tracemalloc
    image=np.full((2048,2048,3),.4,np.float32)
    tracemalloc.start()
    try:
        count=0
        for part in warp_pair_tiles(image,alignment(np.eye(3)),(2048,2048),tile=256):
            assert part.rgb.shape[0]<=256 and part.rgb.shape[1]<=256
            assert not part.rgb.flags.writeable and not part.valid.flags.writeable
            count+=part.valid.size
        _,peak=tracemalloc.get_traced_memory()
    finally:tracemalloc.stop()
    assert count==2048*2048 and peak<32*1024*1024

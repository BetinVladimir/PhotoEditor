import numpy as np
import pytest
from photo_worker.algorithms.body_warp import bilinear, warp_rgb_into
from photo_worker.algorithms.color import to_linear,to_encoded

def field(h,w):
    yy,xx=np.mgrid[:h,:w]
    return np.stack((xx+.5,yy),axis=-1).astype(np.float32)

def test_body_hidden_rgb_does_not_affect_visible_edges():
    alpha=np.zeros((8,8),np.uint16);alpha[:,:4]=65535
    a=np.zeros((8,8,3),np.float32);a[:,:,0]=1
    b=a.copy();b[:,4:]=[0,1,0]
    left=np.empty_like(a);right=np.empty_like(a);inverse=field(8,8)
    warp_rgb_into(a,alpha,inverse,left);warp_rgb_into(b,alpha,inverse,right)
    np.testing.assert_array_equal(left,right)
    assert np.isfinite(left).all() and np.all(left[:,:,1:]==0)

@pytest.mark.parametrize('alpha',[None,65535])
def test_body_opaque_math_matches_existing_sampler(alpha):
    rgb=np.random.default_rng(42).random((9,17,3),dtype=np.float32);inverse=field(9,17)
    output=np.empty_like(rgb)
    opacity=None if alpha is None else np.full((9,17),alpha,np.uint16)
    warp_rgb_into(rgb,opacity,inverse,output)
    expected=to_encoded(np.asarray(bilinear(to_linear(rgb),inverse),np.float32))
    np.testing.assert_array_equal(output,expected)

def test_body_sampler_tiles_use_source_bounds_not_tile_height():
    rgb=np.arange(7*5,dtype=np.float32).reshape(7,5)
    inverse=field(7,5)
    np.testing.assert_array_equal(bilinear(rgb,inverse[5:]),bilinear(rgb,inverse)[5:])

def test_body_cancel_does_not_return_success():
    rgb=np.zeros((4,4,3),np.float32);calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        warp_rgb_into(rgb,None,field(4,4),np.empty_like(rgb),check_active=active)


def test_body_alpha_tiled_sampler_keeps_original_uint16_math():
    from photo_worker.algorithms.body_warp import warp_alpha_into
    alpha=np.random.default_rng(81).integers(0,65536,(300,17),dtype=np.uint16)
    inverse=field(300,17);output=np.empty_like(alpha)
    warp_alpha_into(alpha,inverse,output)
    expected=np.rint(np.clip(bilinear(alpha.astype(np.float32),inverse),0,65535)).astype(np.uint16)
    np.testing.assert_array_equal(output,expected)


def test_body_alpha_cancel_is_observed_between_tiles():
    from photo_worker.algorithms.body_warp import warp_alpha_into
    alpha=np.ones((300,17),np.uint16);calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        warp_alpha_into(alpha,field(300,17),np.empty_like(alpha),check_active=active)


@pytest.mark.parametrize('alpha_mode',['none','opaque','translucent'])
def test_body_preserves_stationary_protected_rgb_exactly(alpha_mode):
    from photo_worker.algorithms.body_warp import identity_inverse_field
    rgb=np.random.default_rng(127).random((300,19,3),dtype=np.float32)
    alpha=None if alpha_mode=='none' else np.full((300,19),65535,np.uint16)
    if alpha_mode=='translucent':
        alpha[:,::2]=0
        alpha[:,1::2]=27123
    inverse=identity_inverse_field(19,300)
    inverse[10:20,5:10,0]+=.25
    output=np.empty_like(rgb)
    warp_rgb_into(rgb,alpha,inverse,output)
    stationary=np.ones((300,19),bool);stationary[10:20,5:10]=False
    np.testing.assert_array_equal(output[stationary],rgb[stationary])
    assert np.isfinite(output).all()

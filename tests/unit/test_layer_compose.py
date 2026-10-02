import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.algorithms.layer_compose import compose_layer_tile

LINEAR=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='a'*64)

def compose(s,p,m,**kwargs):return compose_layer_tile(s,p,m,color=kwargs.pop('color',LINEAR),opacity=kwargs.pop('opacity',1),blend=kwargs.pop('blend','normal'),max_tile_bytes=4096,**kwargs)

@pytest.mark.parametrize('blend,expected',[('normal',.4),('multiply',.325),('screen',.575)])
def test_blending_linear_and_opacity_once(blend,expected):
    s=np.full((1,1,3),.5,np.float32);p=np.full_like(s,.3);mask=np.ones((1,1),np.float32)
    result=compose(s,p,mask,blend=blend,opacity=.5)
    np.testing.assert_allclose(result.rgb,expected,atol=1e-7,rtol=0)
    assert result.alpha is None and result.changed[0,0]==1
    np.testing.assert_array_equal(s,np.full_like(s,.5))

def test_protected_zero_mask_and_zero_opacity_exact_hdr():
    s=np.array([[[-0.,-2.,4.],[.1,.2,.3],[.4,.5,.6]]],np.float32);p=np.ones_like(s)*2
    mask=np.array([[0,1,1]],np.float32);protected=np.array([[0,.0001,0]],np.float32)
    result=compose(s,p,mask,protected=protected)
    np.testing.assert_array_equal(result.rgb[:,:2].view(np.uint32),s[:,:2].view(np.uint32))
    np.testing.assert_array_equal(result.changed,[[0,0,1]])
    bypass=compose(s,p,mask,opacity=0)
    np.testing.assert_array_equal(bypass.rgb.view(np.uint32),s.view(np.uint32))
    assert not bypass.changed.any()

def test_float_alpha_preserved_without_accumulation_and_interpolated():
    s=np.full((1,2,3),.5,np.float32);p=np.full_like(s,.7);m=np.ones((1,2),np.float32)
    a=np.array([[.5,.2]],np.float32)
    same=compose(s,p,m,source_alpha=a,proposal_alpha=a,opacity=.5)
    np.testing.assert_array_equal(same.alpha,a)
    changed=compose(s,p,m,source_alpha=a,proposal_alpha=np.ones_like(a),opacity=.5)
    np.testing.assert_allclose(changed.alpha,[[.75,.6]],atol=1e-7,rtol=0)

def test_signed_gamma_hdr_linear_blend_and_overflow_rejection():
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='b'*64)
    s=np.full((1,1,3),-2,np.float32);p=np.full_like(s,4);m=np.ones((1,1),np.float32)
    result=compose(s,p,m,color=color,opacity=.5)
    expected=((4**2.4-2**2.4)/2)**(1/2.4)
    np.testing.assert_allclose(result.rgb,expected,atol=1e-6,rtol=0)
    with pytest.raises(ValueError,match='OVERFLOW'):compose(np.full_like(s,np.finfo(np.float32).max),np.full_like(s,2),m,blend='multiply')

def test_cancellation_invalid_alpha_and_memory():
    s=np.ones((1,1,3),np.float32);m=np.ones((1,1),np.float32)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):compose(s,s,m,check_active=cancel)
    with pytest.raises(ValueError):compose(s,s,m,source_alpha=np.ones((1,1),np.uint16))
    with pytest.raises(ValueError):compose_layer_tile(s,s,m,color=LINEAR,opacity=1,blend='normal',max_tile_bytes=1)


def test_normal_full_opacity_and_identity_preserve_exact_bits():
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='b'*64)
    s=np.array([[[-0.,.1234567,-2.]]],np.float32);p=np.array([[[.7654321,-.1,4.]]],np.float32);m=np.ones((1,1),np.float32)
    full=compose(s,p,m,color=color)
    np.testing.assert_array_equal(full.rgb.view(np.uint32),p.view(np.uint32))
    identity=compose(s,s,m,color=color,opacity=.25)
    np.testing.assert_array_equal(identity.rgb.view(np.uint32),s.view(np.uint32))
    assert not identity.changed.any()


def test_zero_effect_preserves_implicit_opaque_alpha():
    s=np.ones((1,1,3),np.float32);mask=np.ones((1,1),np.float32)
    result=compose(s,s,mask,opacity=0,proposal_alpha=np.zeros((1,1),np.float32))
    assert result.alpha is None and not result.changed.any()


def test_unchanged_alpha_signed_zero_bits_are_preserved():
    s=np.ones((1,1,3),np.float32);m=np.ones((1,1),np.float32);a=np.array([[-0.]],np.float32)
    result=compose(s,s,m,opacity=.5,source_alpha=a,proposal_alpha=a)
    np.testing.assert_array_equal(result.alpha.view(np.uint32),a.view(np.uint32))
    assert not result.changed.any()

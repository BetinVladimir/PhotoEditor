"""Authored native Region body pixel invariants; qualification remains separate."""
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.operations.region.geometry_native import prepare_region_body
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.algorithms.body_warp import warp_rgb_into,warp_alpha_into


def fixture():
    yy,xx=np.mgrid[:8,:12]
    field=np.stack((xx+.25,yy),axis=-1).astype(np.float32)
    rgb=np.random.default_rng(10).random((8,12,3),dtype=np.float32)
    alpha=np.zeros((8,12),np.uint16);alpha[:,:6]=65535
    source=SimpleNamespace(canvas=SimpleNamespace(width=12,height=8))
    target=SimpleNamespace(canvas=source.canvas,transform_chain=object())
    return source,target,rgb,alpha,field


def test_region_body_matches_shared_native_rgb_and_alpha(tmp_path):
    source,target,rgb,alpha,field=fixture()
    expected=np.empty_like(rgb);opacity=np.empty_like(alpha)
    warp_rgb_into(rgb,alpha,field,expected);warp_alpha_into(alpha,field,opacity)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with prepare_region_body(source,target,SimpleNamespace(applied=True),rgb,alpha,field,scratch) as result:
            np.testing.assert_array_equal(result.rgb,expected)
            np.testing.assert_array_equal(result.alpha,opacity.astype(np.float32)/65535)
            assert result.transform_chain is target.transform_chain


def test_region_body_safety_skip_preserves_original_float_bits(tmp_path):
    source,target,rgb,alpha,field=fixture();rgb[0,0,0]=np.float32(-0.)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with prepare_region_body(source,target,SimpleNamespace(applied=False),rgb,alpha,field,scratch) as result:
            assert result.rgb is rgb and result.alpha is alpha
            np.testing.assert_array_equal(result.rgb.view(np.uint32),rgb.view(np.uint32))


def test_region_body_cancellation_cannot_escape_as_rendered_success(tmp_path):
    source,target,rgb,alpha,field=fixture();calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1000000,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_region_body(source,target,SimpleNamespace(applied=True),rgb,alpha,field,scratch,check_active=active):
                pytest.fail('cancelled body returned pixels')

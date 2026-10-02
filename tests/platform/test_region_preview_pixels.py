import numpy as np
import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.export.encode import _resize_rgba
from photo_worker.operations.region.contracts import RegionPreviewParams
from photo_worker.operations.region.pixels import extract_rendered_region
from test_region_preview_contract import region_fixture


def fixture():
    plan,image=region_fixture()
    plan['request']['rect']=dict(x0=.25,y0=.25,x1=.75,y1=.75)
    plan['request']['max_edge']=128
    plan['geometry']=dict(left=100,top=75,width=200,height=150,output_width=128,output_height=96)
    return RegionPreviewParams.model_validate_json(canonical_bytes(plan)),canonical_bytes(image)


def test_extract_region_matches_existing_resizer_and_owns_output():
    params,body=fixture()
    rgb=np.random.default_rng(2).random((300,400,3),dtype=np.float32)
    expected,_=_resize_rgba(rgb[75:225,100:300],None,128,96,encoding='gamma24')
    out,alpha=extract_rendered_region(params,body,rgb)
    assert out.shape==(96,128,3) and alpha is None
    assert np.array_equal(out,expected)
    assert not np.shares_memory(out,rgb)
    assert out.flags.c_contiguous


def test_no_resize_still_detaches_from_full_frame():
    plan,image=region_fixture()
    params=RegionPreviewParams.model_validate_json(canonical_bytes(plan))
    rgb=np.full((300,400,3),.5,np.float32)
    out,_=extract_rendered_region(params,canonical_bytes(image),rgb)
    assert np.array_equal(out,rgb) and not np.shares_memory(out,rgb)


@pytest.mark.parametrize('rgb',[
    np.zeros((150,200,3),np.float32),np.zeros((300,400,3),np.uint16),
    np.full((300,400,3),np.nan,np.float32),np.full((300,400,3),1.01,np.float32),
])
def test_partial_canvas_or_invalid_values_are_rejected(rgb):
    params,body=fixture()
    with pytest.raises(ValueError,match='REGION_RENDER_INVALID'):extract_rendered_region(params,body,rgb)


def test_cancel_observed_before_scanning_or_resize():
    params,body=fixture()
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        extract_rendered_region(params,body,np.zeros((300,400,3),np.float32),check_active=cancel)


def test_linear_alpha_resize_preserves_visible_color_and_opacity():
    rgb=np.array([[[1.,.5,.25],[0.,0.,0.]]],np.float32)
    alpha=np.array([[1.,0.]],np.float32)
    out,opacity=_resize_rgba(rgb,alpha,1,1,encoding='linear')
    assert np.array_equal(out,np.array([[[1.,.5,.25]]],np.float32))
    assert np.array_equal(opacity,np.array([[.5]],np.float32))


def test_invalid_pixels_outside_requested_roi_are_rejected():
    params,body=fixture()
    rgb=np.zeros((300,400,3),np.float32)
    rgb[0,0,0]=np.nan
    with pytest.raises(ValueError,match='REGION_RENDER_INVALID'):
        extract_rendered_region(params,body,rgb)

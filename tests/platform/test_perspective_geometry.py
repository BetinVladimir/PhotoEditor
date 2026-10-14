"""Unit coverage authored alongside V29 geometry; execution deferred."""
import numpy as np
import pytest
from photo_worker.algorithms.perspective import manual_perspective,warp_perspective_tiles,checked_homography

FULL=[[0,0],[1,0],[1,1],[0,1]]

def test_identity_preserves_hdr_and_half_pixel_centers():
    geometry=manual_perspective(FULL,8,6,max_crop_fraction=0,max_pixels=48)
    rgb=np.arange(144,dtype=np.float32).reshape(6,8,3)/10
    alpha=np.ones((6,8),np.float32)
    tiles=list(warp_perspective_tiles(rgb,alpha,geometry,tile=3))
    result=np.zeros_like(rgb)
    for block in tiles:result[block.y:block.y+block.rgb.shape[0],block.x:block.x+block.rgb.shape[1]]=block.rgb
    np.testing.assert_allclose(result,rgb,atol=1e-6)
    assert geometry.crop_fraction==0

@pytest.mark.parametrize('corners',[FULL[::-1],[[0,0],[1,1],[1,0],[0,1]],[[0,0],[0,0],[1,1],[0,1]]])
def test_invalid_quad_rejected(corners):
    with pytest.raises(ValueError,match='CORNERS_INVALID'):
        manual_perspective(corners,100,100,max_crop_fraction=1,max_pixels=10000)

def test_crop_and_output_caps_precede_render():
    with pytest.raises(ValueError,match='CROP_LIMIT'):
        manual_perspective([[.2,.2],[.8,.2],[.8,.8],[.2,.8]],100,100,max_crop_fraction=.1,max_pixels=10000)
    with pytest.raises(ValueError,match='PIXEL_LIMIT'):
        manual_perspective(FULL,100,100,max_crop_fraction=0,max_pixels=9999)

def test_horizon_crossing_rejected():
    with pytest.raises(ValueError,match='HORIZON'):
        checked_homography([[1,0,0],[0,1,0],[1,0,-.5]],FULL)

def test_transparent_pixels_do_not_leak_color():
    geometry=manual_perspective(FULL,2,2,max_crop_fraction=0,max_pixels=4)
    rgb=np.ones((2,2,3),np.float32)*50
    alpha=np.zeros((2,2),np.float32)
    block=next(warp_perspective_tiles(rgb,alpha,geometry))
    assert not block.rgb.any() and not block.alpha.any()

def test_cancellation_propagates_before_output():
    geometry=manual_perspective(FULL,2,2,max_crop_fraction=0,max_pixels=4)
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        next(warp_perspective_tiles(np.ones((2,2,3),np.float32),np.ones((2,2),np.float32),geometry,check_active=cancel))

from photo_worker.algorithms.perspective import auto_perspective,perspective_coverage

HORIZONTAL=[[[1,2],[7,2]],[[1,4],[7,4]]]
VERTICAL=[[[2,1],[2,5]],[[6,1],[6,5]]]

def test_auto_parallel_families_preserve_identity():
    geometry=auto_perspective(HORIZONTAL,VERTICAL,8,6,max_crop_fraction=.5,max_pixels=100)
    np.testing.assert_allclose(geometry.source_to_output,np.eye(3),atol=1e-10)
    assert geometry.width==8 and geometry.height==6
    assert geometry.crop_fraction==0


def test_auto_endpoint_reversal_does_not_change_geometry():
    first=auto_perspective(HORIZONTAL,VERTICAL,8,6,max_crop_fraction=.5,max_pixels=100)
    second=auto_perspective(np.array(HORIZONTAL)[:,::-1],np.array(VERTICAL)[:,::-1],8,6,max_crop_fraction=.5,max_pixels=100)
    np.testing.assert_allclose(first.source_to_output,second.source_to_output,atol=1e-10)


def test_auto_rejects_duplicate_lines_and_foreign_endpoints():
    with pytest.raises(ValueError,match='DEGENERATE'):
        auto_perspective([HORIZONTAL[0],HORIZONTAL[0]],VERTICAL,8,6,max_crop_fraction=1,max_pixels=100)
    with pytest.raises(ValueError,match='LINES_INVALID'):
        auto_perspective([[[-1,2],[7,2]],HORIZONTAL[1]],VERTICAL,8,6,max_crop_fraction=1,max_pixels=100)


def test_coverage_agrees_with_render_support_across_tiles():
    geometry=manual_perspective([[.1,0],[.9,.1],[1,1],[0,.9]],20,16,max_crop_fraction=1,max_pixels=1000)
    rgb=np.ones((16,20,3),np.float32);alpha=np.ones((16,20),np.float32)
    blocks=list(warp_perspective_tiles(rgb,alpha,geometry,tile=3))
    valid=sum(np.count_nonzero(block.alpha) for block in blocks)
    expected=1-valid/(geometry.width*geometry.height)
    assert perspective_coverage(geometry,20,16,tile=7)==expected
    assert perspective_coverage(geometry,20,16,tile=3)==expected


@pytest.mark.parametrize('corners', [[],[[0,0]],[[0,0],[1,1]]])
def test_homography_requires_nonempty_domain_boundary(corners):
    with pytest.raises(ValueError,match='GEOMETRY_INVALID'):
        checked_homography(np.eye(3),corners)


def test_manual_output_global_pixel_cap_precedes_homography_solve(monkeypatch):
    # A thin source can rotate into an enormous bounding rectangle despite
    # source pixel count being valid. No output allocation/linear solve allowed.
    def unexpected(*args,**kwargs):raise AssertionError('solve before pixel admission')
    monkeypatch.setattr(np.linalg,'solve',unexpected)
    with pytest.raises(ValueError,match='PIXEL_LIMIT'):
        manual_perspective([[0,0],[1,.4],[1,1],[0,.6]],2,100000000,
                           max_crop_fraction=1,max_pixels=2**53-1)


def test_coverage_refuses_oversized_source_before_inverse(monkeypatch):
    from photo_worker.algorithms.perspective import PerspectiveGeometry
    def unexpected(*args,**kwargs):raise AssertionError('inverse before source admission')
    monkeypatch.setattr(np.linalg,'inv',unexpected)
    geometry=PerspectiveGeometry(np.eye(3),1,1,0.)
    with pytest.raises(ValueError,match='LIMIT_INVALID'):
        perspective_coverage(geometry,20001,10000)

import numpy as np
import pytest
from photo_worker.algorithms.semantic_transfer import fit_planar_correspondences,validate_planar_homography


def fit(a,b,**options):
    return fit_planar_correspondences(a,b,reference_shape=(256,256),match_error_px_max=.5,
        match_inlier_min=.8,**options)


def points():
    x,y=np.meshgrid(np.arange(20,220,30),np.arange(20,220,30))
    return np.column_stack((x.ravel(),y.ravel())).astype(np.float64)


def test_planar_translation_matches_and_owns_readonly_matrix():
    a=points();b=a+[3.,7.]
    result=fit(a,b)
    assert result.reason is None and result.inlier_fraction==1
    np.testing.assert_allclose(result.reference_to_target,[[1,0,3],[0,1,7],[0,0,1]],atol=1e-9)
    assert not result.reference_to_target.flags.writeable
    a[:]=0
    assert result.reference_to_target[0,2]==pytest.approx(3)


def test_planar_outliers_report_observed_fraction():
    a=points();b=a+[3.,7.];b[:5]=[[200,50],[20,230],[190,10],[120,250],[250,110]]
    result=fit(a,b)
    assert result.reason is None and result.inlier_fraction==pytest.approx((len(a)-5)/len(a))


@pytest.mark.parametrize('matrix',[
    [[-1,0,250],[0,1,0],[0,0,1]],[[1,0,0],[0,0,0],[0,0,1]],
    [[1,0,0],[0,1,0],[-.01,0,1]]])
def test_planar_safety_rejects_reflection_singularity_horizon(matrix):
    assert not validate_planar_homography(matrix,(256,256))


def test_collinear_and_reflected_matches_are_unsupported():
    a=np.column_stack((np.arange(8),np.arange(8))).astype(float)
    assert fit(a,a).reason=='NO_PLANAR_CORRESPONDENCE'
    a=points();b=a.copy();b[:,0]=255-b[:,0]
    assert fit(a,b).reason=='UNSAFE_PLANAR_TRANSFORM'


def test_planar_match_cancellation_before_native_work():
    def cancel():raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):fit(points(),points(),check_active=cancel)


def test_actual_orb_mutual_matches_feed_translation_ransac():
    import cv2
    from photo_worker.algorithms.semantic_transfer import match_planar_proxies
    rng=np.random.default_rng(2026)
    source=rng.integers(0,256,(256,256),dtype=np.uint8)
    target=cv2.warpAffine(source,np.array([[1,0,4],[0,1,6]],np.float64),(256,256))
    first=match_planar_proxies(source,target)
    second=match_planar_proxies(source,target)
    assert len(first.reference)>=12
    np.testing.assert_array_equal(first.reference,second.reference)
    np.testing.assert_array_equal(first.target,second.target)
    assert not first.reference.flags.writeable and not first.target.flags.writeable
    result=fit(first.reference,first.target)
    assert result.reason is None
    np.testing.assert_allclose(result.reference_to_target,[[1,0,4],[0,1,6],[0,0,1]],atol=.12)


def test_orb_blank_proxy_produces_no_invented_correspondence():
    from photo_worker.algorithms.semantic_transfer import match_planar_proxies
    source=np.zeros((128,128),np.uint8)
    matched=match_planar_proxies(source,source)
    assert matched.reference.shape==matched.target.shape==(0,2)
    assert fit(matched.reference,matched.target).reason=='NO_PLANAR_CORRESPONDENCE'


@pytest.mark.parametrize('shape,dtype',[((2049,8),np.uint8),((128,128,3),np.uint8),((128,128),np.float32)])
def test_orb_requires_bounded_explicit_proxy(shape,dtype):
    from photo_worker.algorithms.semantic_transfer import match_planar_proxies
    with pytest.raises(ValueError,match='SEMANTIC_PROXY_INVALID'):
        match_planar_proxies(np.zeros(shape,dtype),np.zeros((128,128),np.uint8))


def test_orb_cancellation_restores_native_thread_configuration():
    import cv2
    from photo_worker.algorithms.semantic_transfer import match_planar_proxies
    previous=cv2.getNumThreads();calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==4:raise InterruptedError('cancelled')
    with pytest.raises(InterruptedError):
        match_planar_proxies(np.zeros((128,128),np.uint8),np.zeros((128,128),np.uint8),check_active=active)
    assert cv2.getNumThreads()==previous


def test_linear_proxy_luma_hdr_and_source_bit_ownership():
    from photo_worker.algorithms.semantic_transfer import planar_linear_proxy
    rgb=np.full((8,9,3),.5,np.float32);rgb[0,0]=[-2,0,0];rgb[0,1]=[5,5,5]
    original=rgb.view(np.uint32).copy()
    result=planar_linear_proxy(rgb)
    assert result.pixels[0,0]==0 and result.pixels[0,1]==255 and result.pixels[1,1]==128
    np.testing.assert_array_equal(result.source_to_proxy,np.eye(3))
    np.testing.assert_array_equal(rgb.view(np.uint32),original)
    assert not result.pixels.flags.writeable


def test_linear_proxy_resize_records_half_pixel_centres():
    from photo_worker.algorithms.semantic_transfer import planar_linear_proxy
    result=planar_linear_proxy(np.full((16,4096,3),.3,np.float32))
    assert result.pixels.shape==(8,2048)
    np.testing.assert_array_equal(result.source_to_proxy,[[.5,0,-.25],[0,.5,-.25],[0,0,1]])


def test_actual_linear_image_matching_recovers_full_canvas_translation():
    import cv2
    from photo_worker.algorithms.semantic_transfer import match_planar_linear_images
    rng=np.random.default_rng(106)
    luma=rng.random((256,256),dtype=np.float32)
    rgb=np.repeat(luma[:,:,None],3,axis=2)
    target=cv2.warpAffine(rgb,np.array([[1,0,4],[0,1,6]],float),(256,256))
    result=match_planar_linear_images(rgb,target,match_error_px_max=.5,match_inlier_min=.8)
    assert result.reason is None
    np.testing.assert_allclose(result.reference_to_target,[[1,0,4],[0,1,6],[0,0,1]],atol=.15)


def test_proxy_homography_is_conjugated_for_different_full_canvas_sizes(monkeypatch):
    from photo_worker.algorithms import semantic_transfer as module
    empty=np.empty((0,2),np.float64)
    monkeypatch.setattr(module,'match_planar_proxies',lambda *args,**kwargs:module.PlanarCorrespondences(empty,empty))
    monkeypatch.setattr(module,'fit_planar_correspondences',lambda *args,**kwargs:module.PlanarMatch(np.eye(3),1.,None))
    source=np.full((32,4096,3),.3,np.float32)
    target=np.full((16,2048,3),.3,np.float32)
    result=module.match_planar_linear_images(source,target,match_error_px_max=.5,match_inlier_min=.8)
    np.testing.assert_array_equal(result.reference_to_target,[[.5,0,-.25],[0,.5,-.25],[0,0,1]])


def test_tiled_planar_mask_translation_matches_native_warp_without_input_mutation():
    import cv2
    from photo_worker.algorithms.semantic_transfer import warp_planar_mask_into
    source=np.zeros((533,701),np.float32);source[20:400,30:610]=.7
    original=source.copy();matrix=np.array([[1,0,4.25],[0,1,6.5],[0,0,1]],float)
    output=np.empty_like(source);protection=np.zeros_like(source)
    warp_planar_mask_into(source,matrix,output,source_protected=protection,target_protected=protection)
    expected=cv2.warpPerspective(source,matrix,(701,533),flags=cv2.INTER_LINEAR)
    np.testing.assert_array_equal(output,expected)
    np.testing.assert_array_equal(source,original)


@pytest.mark.parametrize('side',['source','target'])
def test_planar_mask_positive_protection_rejects(side):
    from photo_worker.algorithms.semantic_transfer import warp_planar_mask_into
    source=np.ones((16,16),np.float32);zero=np.zeros_like(source);protected=zero.copy();protected[8,8]=.001
    with pytest.raises(ValueError,match='SEMANTIC_PROTECTED_OVERLAP'):
        warp_planar_mask_into(source,np.eye(3),np.empty_like(source),
            source_protected=protected if side=='source' else zero,
            target_protected=protected if side=='target' else zero)


def test_planar_mask_rejects_borrowed_output_alias():
    from photo_worker.algorithms.semantic_transfer import warp_planar_mask_into
    source=np.ones((16,16),np.float32);zero=np.zeros_like(source)
    with pytest.raises(ValueError,match='ALIASED'):
        warp_planar_mask_into(source,np.eye(3),source,source_protected=zero,target_protected=zero)


def test_planar_mask_handles_canvas_wider_than_native_remap_limit():
    from photo_worker.algorithms.semantic_transfer import warp_planar_mask_into
    source=np.full((8,40000),.3,np.float32);zero=np.zeros_like(source);output=np.empty_like(source)
    warp_planar_mask_into(source,np.eye(3),output,source_protected=zero,target_protected=zero)
    np.testing.assert_array_equal(output,source)

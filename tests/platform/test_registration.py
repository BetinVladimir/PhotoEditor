import cv2
import numpy as np
import pytest
from photo_worker.algorithms.registration import register_pair,warp_pair_before

def scene():
    image=np.zeros((256,384,3),np.float32)
    rng=np.random.default_rng(8)
    for _ in range(100):
        x,y=rng.integers([10,10],[374,246]);radius=int(rng.integers(2,9))
        cv2.circle(image,(int(x),int(y)),radius,tuple(float(x) for x in rng.uniform(.1,1,3)),-1)
    return image

def test_similarity_registration_and_warp():
    before=scene();matrix=cv2.getRotationMatrix2D((192,128),1.,1.)
    matrix[:,2]+=[2,-1]
    after=cv2.warpAffine(before,matrix,(384,256))
    alignment=register_pair(before,after,seed=19,background_mask=np.ones(before.shape[:2]))
    assert alignment.overlap>=.9 and alignment.median_reprojection<1
    points=np.array([[20,20,1],[200,140,1],[350,220,1]],np.float64)
    assert np.max(np.linalg.norm(points@alignment.before_to_after[:2].T-points@matrix.T,axis=1))<.5
    output,valid=warp_pair_before(before,alignment,after.shape[:2])
    assert output.shape==after.shape and valid.dtype==bool

def test_manual_crop_and_reflection_rejection():
    before=scene();after=before[:,100:]
    matrix=np.array([[1,0,-100],[0,1,0],[0,0,1]],np.float64)
    alignment=register_pair(before,after,manual_matrix=matrix,crop_aware=True)
    assert alignment.confidence=='user_confirmed'
    warped,valid=warp_pair_before(before,alignment,after.shape[:2])
    assert np.max(np.abs(warped[valid]-after[valid]))<2/65535
    with pytest.raises(ValueError,match='NOT_SIMILARITY'):
        register_pair(before,after,manual_matrix=np.diag([-1,1,1]))

def test_alignment_fails_without_evidence():
    with pytest.raises(ValueError,match='FEATURES_INSUFFICIENT'):
        register_pair(np.ones((32,32,3),np.float32),np.ones((32,32,3),np.float32))


def test_different_preview_rounding_still_publishes_a_similarity():
    from photo_worker.algorithms.registration import _similarity
    before=cv2.resize(scene(),(1031,769))
    matrix=cv2.getRotationMatrix2D((515,384),.4,1.)
    matrix[:,2]+=[2,1]
    after=cv2.warpAffine(before,matrix,(1037,773))
    result=register_pair(before,after,seed=19)
    _similarity(result.before_to_after)
    assert result.overlap>=.9 and result.median_reprojection<=1
    points=np.array([[80,80,1],[515,384,1],[900,600,1]],np.float64)
    assert np.max(np.linalg.norm(points@result.before_to_after[:2].T-points@matrix.T,axis=1))<1.5


def test_cancellation_propagates_from_registration():
    def cancelled():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):register_pair(scene(),scene(),check_active=cancelled)

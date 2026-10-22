import numpy as np
import pytest
from photo_worker.algorithms.body_warp import solve_inverse_field

def anchors():
    return (np.array([[10,10],[30,10],[10,30],[30,30]],np.float64),
            np.tile([.3,0],(4,1)),np.array([8,8,32,32],np.float64))

def test_zero_person_gate_keeps_exact_identity_even_with_nonzero_rbf():
    h,w=45,50;person=np.zeros((h,w),np.float32)
    result=solve_inverse_field(w,h,[anchors()],person)
    yy,xx=np.mgrid[:h,:w]
    np.testing.assert_array_equal(result,np.stack((xx,yy),axis=-1).astype(np.float32))

def test_rbf_only_modifies_its_roi_and_keeps_overlap_refusal():
    person=np.ones((45,50),np.float32)
    result=solve_inverse_field(50,45,[anchors()],person)
    yy,xx=np.mgrid[:45,:50];identity=np.stack((xx,yy),axis=-1).astype(np.float32)
    np.testing.assert_array_equal(result[:8],identity[:8])
    assert np.isfinite(result).all() and np.any(result[8:33,8:33]!=identity[8:33,8:33])
    with pytest.raises(ValueError,match='AMBIGUOUS_OVERLAP'):
        solve_inverse_field(50,45,[anchors(),anchors()],person)

def test_rbf_cancel_between_queries_does_not_return_partial_field():
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==4:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        solve_inverse_field(50,45,[anchors()],np.ones((45,50),np.float32),check_active=active)


def test_identity_safety_metrics_across_tile_seams():
    from photo_worker.algorithms.body_warp import validate_field
    h,w=515,519;yy,xx=np.mgrid[:h,:w]
    inverse=np.stack((xx,yy),axis=-1).astype(np.float32)
    check=validate_field(inverse,1,np.zeros((h,w),np.float32))
    assert check.safe and check.max_displacement==0 and check.min_jacobian==1
    assert check.protected_center_displacement==0


def test_safety_rejects_fold_and_protected_background_shift():
    from photo_worker.algorithms.body_warp import validate_field
    yy,xx=np.mgrid[:300,:301];inverse=np.stack((xx,yy),axis=-1).astype(np.float32)
    shifted=inverse.copy();shifted[...,0]+=.5
    assert not validate_field(shifted,1,np.zeros((300,301),np.float32)).safe
    folded=inverse.copy();folded[...,0]*=-1
    assert not validate_field(folded,1000,np.ones((300,301),np.float32)).safe


def test_safety_validation_observes_cancellation_between_tiles():
    from photo_worker.algorithms.body_warp import validate_field
    yy,xx=np.mgrid[:300,:301];inverse=np.stack((xx,yy),axis=-1).astype(np.float32);calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        validate_field(inverse,1,np.ones((300,301),np.float32),check_active=active)


def test_identity_skip_field_is_exact_without_full_frame_grid(monkeypatch):
    from photo_worker.algorithms.body_warp import identity_inverse_field
    def refuse_grid(*args,**kwargs):raise AssertionError('full frame grid forbidden')
    monkeypatch.setattr(np,'mgrid',refuse_grid)
    result=identity_inverse_field(519,515)
    assert result.dtype==np.float32 and result.shape==(515,519,2)
    np.testing.assert_array_equal(result[...,0],np.broadcast_to(np.arange(519),(515,519)))
    np.testing.assert_array_equal(result[...,1],np.broadcast_to(np.arange(515)[:,None],(515,519)))


def test_identity_skip_field_cancels_between_rows():
    from photo_worker.algorithms.body_warp import identity_inverse_field
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        identity_inverse_field(519,515,check_active=active)


@pytest.mark.parametrize('width,height',[(0,2),(2,0),(True,2),(40000001,1)])
def test_identity_field_rejects_invalid_or_overbudget_dimensions(width,height):
    from photo_worker.algorithms.body_warp import identity_inverse_field
    with pytest.raises(ValueError,match='BODY_FIELD_LAYOUT_INVALID'):
        identity_inverse_field(width,height)

import numpy as np
import pytest
from photo_worker.operations.optical.color import to_working_linear
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.semantic.exposure_encoding import restore_local_exposure_encoding


def fixture():
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='a'*64)
    original=np.array([[[.1,.2,.3],[.7,.8,1.2],[.4,.5,.6]]],np.float32)
    mask=np.array([[0.,.5,0.]],np.float32);protected=np.array([[1.,0.,0.]],np.float32)
    linear=to_working_linear(original,color);linear[:,1]*=2
    return original,linear,mask,protected,color


def test_only_masked_changed_pixels_reencode_without_hdr_clipping():
    original,linear,mask,protected,color=fixture()
    result=restore_local_exposure_encoding(original,linear,mask,protected,color,max_bytes=576,check_active=lambda:None)
    np.testing.assert_array_equal(result[:,[0,2]],original[:,[0,2]])
    np.testing.assert_allclose(result[:,1],original[:,1]*2**(1/2.4),rtol=1e-6)
    assert result[0,1,2]>1


def test_noop_is_bit_exact_even_inside_mask():
    original,_,mask,protected,color=fixture()
    result=restore_local_exposure_encoding(original,to_working_linear(original,color),mask,protected,color,max_bytes=576,check_active=lambda:None)
    np.testing.assert_array_equal(result,original)


def test_budget_and_protected_overlap_fail():
    args=fixture()
    with pytest.raises(ValueError,match='RESOURCE_PROFILE'):
        restore_local_exposure_encoding(*args,max_bytes=575,check_active=lambda:None)
    args[3][0,1]=.001
    with pytest.raises(ValueError,match='PROTECTED_OVERLAP'):
        restore_local_exposure_encoding(*args,max_bytes=576,check_active=lambda:None)


def test_final_cancellation_never_returns_encoded_output():
    calls=[]
    def active():
        calls.append(True)
        if len(calls)==3:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        restore_local_exposure_encoding(*fixture(),max_bytes=576,check_active=active)

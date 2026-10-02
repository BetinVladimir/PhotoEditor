import numpy as np
import pytest
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.optical.color import to_working_linear,from_working_linear


def test_signed_gamma_preserves_hdr_primaries_and_input():
    color=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='a'*64)
    rgb=np.array([[[-.5,2.,.25],[0.,-0.,1.]]],np.float32)
    original=rgb.copy();linear=to_working_linear(rgb,color)
    assert linear[0,0,0]<0 and linear[0,0,1]>1
    np.testing.assert_allclose(from_working_linear(linear,color),rgb,rtol=1e-6,atol=1e-7)
    np.testing.assert_array_equal(original.view(np.uint32),rgb.view(np.uint32))
    assert np.signbit(linear[0,1,1])
    assert np.signbit(from_working_linear(linear,color)[0,1,1])


def test_linear_copy_and_nonfinite_or_overflow_refusal():
    color=ColorSpace(profile_id='linear_srgb_v1',encoding='linear',icc_sha256='b'*64)
    rgb=np.array([[[4.,-2.,3.]]],np.float32)
    output=to_working_linear(rgb,color)
    np.testing.assert_array_equal(output,rgb)
    assert not np.shares_memory(output,rgb)
    with pytest.raises(ValueError,match='COLOR_INVALID'):to_working_linear(rgb.astype(np.float64),color)
    invalid=rgb.copy();invalid[0,0,0]=np.nan
    with pytest.raises(ValueError,match='COLOR_INVALID'):to_working_linear(invalid,color)
    gamma=ColorSpace(profile_id='work_rec2020_gamma24_v1',encoding='gamma24',icc_sha256='a'*64)
    huge=np.full((1,1,3),np.finfo(np.float32).max,np.float32)
    with pytest.raises(ValueError,match='COLOR_OVERFLOW'):to_working_linear(huge,gamma)

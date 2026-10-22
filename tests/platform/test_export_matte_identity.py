"""Matte endpoints and fractional linear-light math; execution deferred."""
import numpy as np
import pytest
from photo_worker.operations.export.encode import _composite_matte


@pytest.mark.parametrize('profile',['srgb_v1','work_rec2020_gamma24_v1'])
def test_mixed_matte_preserves_exact_opaque_source_and_background(profile):
    rgb=np.array([[[.137,.421,.793],[.913,.217,.631],[.391,.517,.119]]],np.float32)
    alpha=np.array([[1,0,.5]],np.float32)
    matte=np.array([.173,.347,.719],np.float32)
    before=rgb.copy()
    output=_composite_matte(rgb,alpha,matte.tolist(),profile)
    np.testing.assert_array_equal(output[0,0],rgb[0,0])
    np.testing.assert_array_equal(output[0,1],matte)
    if profile=='srgb_v1':
        decode=lambda v:np.where(v<=.04045,v/12.92,((v+.055)/1.055)**2.4)
        encode=lambda v:np.where(v<=.0031308,v*12.92,1.055*v**(1/2.4)-.055)
    else:
        decode=lambda v:v**2.4
        encode=lambda v:v**(1/2.4)
    expected=encode((decode(rgb[0,2])+decode(matte))*.5)
    np.testing.assert_allclose(output[0,2],expected,rtol=0,atol=1e-7)
    np.testing.assert_array_equal(rgb,before)
    assert not np.shares_memory(output,rgb)


def test_mixed_matte_keeps_existing_unit_domain_clamp():
    rgb=np.array([[[-1,2,.375],[.1,.2,.3]]],np.float32)
    output=_composite_matte(rgb,np.array([[1,0]],np.float32),[.2,.4,.6],'srgb_v1')
    np.testing.assert_array_equal(output[0,0],np.array([0,1,.375],np.float32))

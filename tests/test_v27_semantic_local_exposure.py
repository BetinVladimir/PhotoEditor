import numpy as np
import pytest
from photo_worker.operations.semantic.local_exposure import apply_local_exposure


def fixture():
    rgb=np.array([[[.25,.5,1.5],[.2,.4,.8]],[[0.,.125,.75],[.1,.3,.6]]],dtype=np.float32)
    mask=np.array([[1.,.5],[0.,0.]],dtype=np.float32)
    protected=np.array([[0.,0.],[1.,0.]],dtype=np.float32)
    return rgb,mask,protected


@pytest.mark.parametrize('ev,strength,gains',[(1.,.5,(1.4142135623730951,1.189207115002721)),(-1.,1.,(.5,.7071067811865476))])
def test_exposure_uses_linear_ev_field_without_clipping_or_unmasked_changes(ev,strength,gains):
    rgb,mask,protected=fixture()
    original=rgb.copy()
    result=apply_local_exposure(rgb,mask,protected,ev=ev,strength=strength,max_bytes=4*96)
    np.testing.assert_allclose(result[0],rgb[0]*np.array(gains,dtype=np.float32)[:,None],rtol=1e-7)
    if ev>0:assert result[0,0,2]>1 # HDR values remain available to the existing tone pipeline.
    assert result[1].tobytes()==original[1].tobytes()
    assert rgb.tobytes()==original.tobytes()
    assert not np.shares_memory(result,rgb)
    result[:]=0
    assert rgb.tobytes()==original.tobytes()


@pytest.mark.parametrize('ev,strength',[(0.,1.),(1.,0.),(-1.,0.)])
def test_explicit_zero_adjustment_preserves_all_input_bits(ev,strength):
    rgb,mask,protected=fixture()
    result=apply_local_exposure(rgb,mask,protected,ev=ev,strength=strength,max_bytes=4*96)
    assert result.tobytes()==rgb.tobytes()


@pytest.mark.parametrize('mutation', ['overlap','nan','mask-range','dtype','shape','budget','ev','strength'])
def test_invalid_or_unprotected_edit_produces_no_result(mutation):
    rgb,mask,protected=fixture()
    arguments=dict(ev=1.,strength=.5,max_bytes=4*96)
    if mutation=='overlap':protected[0,0]=.01
    elif mutation=='nan':rgb[0,0,0]=np.nan
    elif mutation=='mask-range':mask[0,0]=1.1
    elif mutation=='dtype':rgb=rgb.astype(np.float64)
    elif mutation=='shape':mask=mask[:1]
    elif mutation=='budget':arguments['max_bytes']-=1
    elif mutation=='ev':arguments['ev']=1.01
    elif mutation=='strength':arguments['strength']=True
    with pytest.raises(ValueError):
        apply_local_exposure(rgb,mask,protected,**arguments)


def test_overflow_is_denied_instead_of_publishing_infinite_pixels():
    rgb,mask,protected=fixture()
    rgb[0,0]=np.finfo(np.float32).max
    with pytest.raises(ValueError,match='NONFINITE_OUTPUT'):
        apply_local_exposure(rgb,mask,protected,ev=1.,strength=1.,max_bytes=4*96)


def test_budget_denial_precedes_owned_image_allocation(monkeypatch):
    rgb,mask,protected=fixture()
    def forbidden(*args,**kwargs):
        raise AssertionError('image allocation before budget admission')
    monkeypatch.setattr(np,'array',forbidden)
    with pytest.raises(ValueError,match='RESOURCE_PROFILE'):
        apply_local_exposure(rgb,mask,protected,ev=1.,strength=1.,max_bytes=4*96-1)


def test_callback_cannot_substitute_owned_source_or_mask():
    rgb,mask,protected=fixture()
    expected=apply_local_exposure(rgb,mask,protected,ev=1.,strength=.5,max_bytes=4*96)
    def mutate():
        rgb[:]=0
        mask[:]=1
        protected[:]=1
    result=apply_local_exposure(rgb,mask,protected,ev=1.,strength=.5,max_bytes=4*96,check_active=mutate)
    assert result.tobytes()==expected.tobytes()


@pytest.mark.parametrize('stop_at',[1,2,4,7,8])
def test_cancellation_propagates_without_returning_partial_result(stop_at):
    rgb=np.full((1,513,3),.25,dtype=np.float32)
    mask=np.ones((1,513),dtype=np.float32)
    protected=np.zeros_like(mask)
    calls=0
    class Cancelled(Exception):pass
    def check():
        nonlocal calls
        calls+=1
        if calls==stop_at:raise Cancelled()
    with pytest.raises(Cancelled):
        apply_local_exposure(rgb,mask,protected,ev=1.,strength=1.,max_bytes=513*96,check_active=check)
    assert calls==stop_at
    np.testing.assert_array_equal(rgb,np.full_like(rgb,.25))


def test_tiling_preserves_exact_unmasked_pixels_across_boundaries():
    rgb=np.full((257,257,3),.25,dtype=np.float32)
    mask=np.zeros((257,257),dtype=np.float32)
    mask[255:257,255:257]=1
    protected=np.zeros_like(mask)
    result=apply_local_exposure(rgb,mask,protected,ev=1.,strength=1.,max_bytes=257*257*96)
    np.testing.assert_array_equal(result[255:257,255:257],np.full((2,2,3),.5,dtype=np.float32))
    assert result[mask==0].tobytes()==rgb[mask==0].tobytes()


@pytest.mark.parametrize('field,value',[('ev',10**1000),('strength',10**1000),('max_bytes',2**53),('ev',float('inf')),('strength',float('nan'))])
def test_extreme_scalar_input_is_a_typed_validation_denial(field,value):
    rgb,mask,protected=fixture()
    arguments=dict(ev=1.,strength=.5,max_bytes=4*96)
    arguments[field]=value
    with pytest.raises(ValueError,match='PARAMETERS_INVALID'):
        apply_local_exposure(rgb,mask,protected,**arguments)


def test_float32_underflow_policy_is_independent_of_numpy_global_settings():
    rgb=np.full((1,1,3),np.nextafter(np.float32(0),np.float32(1)),dtype=np.float32)
    mask=np.ones((1,1),dtype=np.float32)
    protected=np.zeros_like(mask)
    with np.errstate(under='raise'):
        result=apply_local_exposure(rgb,mask,protected,ev=-1.,strength=1.,max_bytes=96)
    assert np.isfinite(result).all()
    assert result.dtype==np.float32


def test_memory_admission_estimate_and_shape_bounds():
    from photo_worker.operations.semantic.local_exposure import local_exposure_memory
    assert local_exposure_memory((4000,6000))==2_304_000_000
    assert local_exposure_memory((1,200_000_000))==19_200_000_000
    for shape in ((0,1),(1,200_000_001),(True,1),(1.0,1),(1,),None):
        with pytest.raises(ValueError):local_exposure_memory(shape)

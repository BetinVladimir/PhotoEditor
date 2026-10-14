import numpy as np
import pytest
from photo_worker.contracts.manual_mask import Stroke,Polygon
from photo_worker.operations.mask.verify import verify_raster
from test_manual_mask import document,point,collect


@pytest.mark.parametrize('tile',[256,512,1024])
def test_original_soft_shapes_verify_without_mutating_outputs(tile):
    mask=document([
        Polygon(kind='polygon',sequence=0,mode='add',points=[point(.05,.1),point(.9,.2),point(.7,.95)]),
        Stroke(kind='stroke',sequence=1,mode='subtract',points=[point(.2,.2),point(.8,.7)],radius=.1,hardness=.4),
        Stroke(kind='stroke',sequence=2,mode='protect',points=[point(.5,.5)],radius=.06,hardness=.5),
    ])
    target,protected=collect(mask,256)
    before=(target.copy(),protected.copy())
    target.setflags(write=False);protected.setflags(write=False)
    assert verify_raster(mask,'a'*64,target,protected,tile=tile)
    assert np.array_equal(target,before[0]) and np.array_equal(protected,before[1])


def test_verification_cancellation_propagates_and_does_not_mutate():
    mask=document([])
    target,protected=collect(mask,256)
    target.setflags(write=False);protected.setflags(write=False)
    calls=0
    failure=RuntimeError('cancelled')
    def active():
        nonlocal calls
        calls+=1
        if calls==6:raise failure
    with pytest.raises(RuntimeError) as error:verify_raster(mask,'a'*64,target,protected,tile=256,check_active=active)
    assert error.value is failure and calls==6 and not np.any(target) and not np.any(protected)


def test_verification_refuses_mutable_arrays_and_changed_source():
    mask=document([])
    target,protected=collect(mask,256)
    with pytest.raises(ValueError,match='INVALID_RASTER_OUTPUT'):verify_raster(mask,'a'*64,target,protected)
    target.setflags(write=False);protected.setflags(write=False)
    with pytest.raises(ValueError,match='OVERRIDE_SOURCE_MISMATCH'):verify_raster(mask,'b'*64,target,protected)

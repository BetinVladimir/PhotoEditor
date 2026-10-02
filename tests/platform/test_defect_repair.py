import numpy as np
import pytest
from photo_worker.algorithms.defect_repair import repair_accepted


def test_float32_constant_precision_without_uint8_quantization():
    rgb=np.full((33,33,3),.400123,np.float32)
    accepted=np.zeros((33,33),bool);accepted[14:19,14:19]=True
    result=repair_accepted(rgb,accepted,np.zeros((33,33),np.float32),8.)
    assert np.max(np.abs(result.rgb-rgb))<2/65535
    assert np.array_equal(result.rgb[~accepted],rgb[~accepted])
    assert result.rgb.dtype==np.float32 and not result.rgb.flags.writeable


def test_approved_spot_repair_and_permanent_protection():
    rgb=np.full((40,40,3),.5,np.float32);rgb[18:22,18:22]=.1
    accepted=np.zeros((40,40),bool);accepted[18:22,18:22]=True
    protected=np.zeros((40,40),np.float32);protected[18,18]=1
    result=repair_accepted(rgb,accepted,protected,4.)
    assert result.rgb[18,18,0]==.1 and np.array_equal(result.rgb[~accepted],rgb[~accepted])
    assert np.mean(result.rgb[19:22,19:22])>.3 and result.applied_pixels==15
    unchanged=repair_accepted(rgb,accepted,protected,4.,strength=0.)
    assert np.array_equal(unchanged.rgb,rgb) and unchanged.applied_pixels==0


def test_context_validation_and_channel_cancellation():
    rgb=np.full((20,20,3),.5,np.float32);mask=np.zeros((20,20),bool);protected=np.zeros((20,20),np.float32)
    mask[0,5]=True
    with pytest.raises(ValueError,match='CONTEXT'):repair_accepted(rgb,mask,protected,4.)
    mask[:]=False;mask[9:11,9:11]=True
    calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==3:raise RuntimeError('cancelled')
    before=rgb.copy()
    with pytest.raises(RuntimeError,match='cancelled'):repair_accepted(rgb,mask,protected,4.,check_active=active)
    assert np.array_equal(rgb,before)

import numpy as np
import pytest
from photo_worker.operations.layers.output_validation import validate_generated_coverage


def test_inherited_dependencies_and_protection_across_tiles():
    mask=np.ones((513,513),np.float32)
    source=np.ones_like(mask);proposal=np.zeros_like(mask);proposal[512,512]=1
    protected=np.zeros_like(mask);protected[0,0]=.1
    output=np.zeros_like(mask);output[0,0]=1;output[512,512]=1
    calls=[]
    validate_generated_coverage(output,mask,opacity=1,blend='normal',source=source,
        proposal=proposal,protected=protected,check_active=lambda:calls.append(True))
    assert len(calls)>=6
    output[512,512]=0
    with pytest.raises(ValueError,match='LAYER_GENERATED_COVERAGE_INVALID'):
        validate_generated_coverage(output,mask,opacity=1,blend='normal',source=source,proposal=proposal,protected=protected)


@pytest.mark.parametrize('invalid',[np.nan,np.inf,-1,2])
def test_invalid_probability_is_not_clamped_into_accepted_evidence(invalid):
    mask=np.array([[invalid]],np.float32)
    with pytest.raises(ValueError,match='LAYER_COVERAGE_MASK_INVALID'):
        validate_generated_coverage(None,mask,opacity=1,blend='normal')


def test_missing_generated_output_and_fractional_binary_rejected():
    mask=np.ones((1,1),np.float32);parent=mask.copy()
    with pytest.raises(ValueError,match='LAYER_GENERATED_BINDING_INVALID'):
        validate_generated_coverage(None,mask,opacity=1,blend='normal',proposal=parent)
    parent[:]=.5
    with pytest.raises(ValueError,match='LINEAGE_MASK_NOT_BINARY'):
        validate_generated_coverage(mask,mask,opacity=1,blend='normal',proposal=parent)


def test_cancellation_propagates():
    def cancel():raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        validate_generated_coverage(None,np.ones((1,1),np.float32),opacity=1,blend='normal',check_active=cancel)

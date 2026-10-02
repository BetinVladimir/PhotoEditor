import pytest
from photo_worker.operations.region.resources import region_source_resources,admit_region_source
from photo_worker.runtime.executor import OperationFailure


def test_region_source_budget_counts_full_canvas_and_alpha_conversion():
    opaque=region_source_resources(400,300,has_alpha=False,alpha_mode='normalize_uint16')
    alpha=region_source_resources(400,300,has_alpha=True,alpha_mode='require_float32')
    legacy=region_source_resources(400,300,has_alpha=True,alpha_mode='normalize_uint16')
    assert opaque['pixel_count']==120000
    assert opaque['normalization_bytes']==0
    assert alpha['scratch_bytes']-opaque['scratch_bytes']==120000*4+4096
    assert legacy['scratch_bytes']-alpha['scratch_bytes']==120000*4+4096
    assert legacy['ram_bytes']-alpha['ram_bytes']==120000*4+4096
    assert admit_region_source(dict(resource_estimate=legacy),400,300,has_alpha=True,alpha_mode='normalize_uint16')==legacy


@pytest.mark.parametrize('key',['ram_bytes','scratch_bytes','pixel_count'])
def test_region_source_admission_rejects_undersized_estimates(key):
    estimate=region_source_resources(400,300,has_alpha=True,alpha_mode='normalize_uint16')
    estimate[key]-=1
    with pytest.raises(OperationFailure):admit_region_source(dict(resource_estimate=estimate),400,300,has_alpha=True,alpha_mode='normalize_uint16')


@pytest.mark.parametrize('width,height,alpha,mode',[(0,1,False,'require_float32'),(True,1,False,'require_float32'),(200000001,1,False,'require_float32'),(1,1,1,'require_float32'),(1,1,False,'bad')])
def test_region_source_resource_shape_and_policy_are_strict(width,height,alpha,mode):
    with pytest.raises(ValueError):region_source_resources(width,height,has_alpha=alpha,alpha_mode=mode)


@pytest.mark.parametrize('value',[True,1.5,2**53,-1,None])
def test_region_source_budget_rejects_noninteroperable_values(value):
    estimate=region_source_resources(400,300,has_alpha=False,alpha_mode='require_float32')
    estimate['ram_bytes']=value
    with pytest.raises(OperationFailure):admit_region_source(dict(resource_estimate=estimate),400,300,has_alpha=False,alpha_mode='require_float32')

"""Nested provider admission must use the sealed composition execution choices."""
import pytest
from photo_worker.v2.render_resources import render_resources, admit_render
from photo_worker.runtime.executor import OperationFailure

PEAK=dict(ram_bytes=1<<20,scratch_bytes=1<<20,vram_bytes=0,output_bytes=0)


@pytest.mark.parametrize('tile',[256,512,1024])
@pytest.mark.parametrize('alpha',['require_float32','normalize_uint16'])
def test_implicit_provider_admission_uses_sealed_choices(tile,alpha):
    required=render_resources(1200,1100,mask_refs=0,mask_nodes=0,provider_peak=PEAK,tile_size=tile,alpha_mode=alpha)
    profile=dict(tile_size=tile,alpha_mode=alpha,resource_estimate=required)
    assert admit_render(profile,1200,1100,mask_refs=0,mask_nodes=0,provider_peak=PEAK)==required
    insufficient=dict(required);insufficient['ram_bytes']-=1
    with pytest.raises(OperationFailure) as error:
        admit_render(dict(profile,resource_estimate=insufficient),1200,1100,mask_refs=0,mask_nodes=0,provider_peak=PEAK)
    assert 'RAM_ADMISSION_EXCEEDED' in str(error.value)


@pytest.mark.parametrize('override',[dict(tile_size=512),dict(alpha_mode='require_float32')])
def test_explicit_choices_cannot_disagree_with_sealed_profile(override):
    required=render_resources(1200,1100,mask_refs=0,mask_nodes=0,provider_peak=PEAK,tile_size=256,alpha_mode='normalize_uint16')
    profile=dict(tile_size=256,alpha_mode='normalize_uint16',resource_estimate=required)
    with pytest.raises(OperationFailure) as error:
        admit_render(profile,1200,1100,mask_refs=0,mask_nodes=0,provider_peak=PEAK,**override)
    assert 'CONFIG_INVALID' in str(error.value)

import pytest
from photo_worker.v2.render_resources import render_resources


def test_resources_include_provider_peak_and_shared_mask_count():
    provider=dict(ram_bytes=123,scratch_bytes=234,vram_bytes=345,output_bytes=456)
    base=render_resources(6000,4000,mask_refs=1,mask_nodes=2,provider_peak=dict.fromkeys(provider,0))
    result=render_resources(6000,4000,mask_refs=1,mask_nodes=2,provider_peak=provider)
    assert result['pixel_count']==24000000
    for key,value in provider.items():assert result[key]==base[key]+value
    additional=render_resources(6000,4000,mask_refs=2,mask_nodes=2,provider_peak=provider)
    assert additional['scratch_bytes']-result['scratch_bytes']==4*24000000+8192
    with pytest.raises(ValueError):render_resources(6000,4000,mask_refs=1,mask_nodes=2,provider_peak={})
    with pytest.raises(ValueError):render_resources(6000,4000,mask_refs=1,mask_nodes=2,provider_peak=dict.fromkeys(provider,2**53-1))


@pytest.mark.parametrize('key',['ram_bytes','scratch_bytes','vram_bytes','output_bytes'])
def test_admission_rejects_each_insufficient_resource(key):
    from photo_worker.v2.render_resources import admit_render
    from photo_worker.runtime.executor import OperationFailure
    args=dict(mask_refs=1,mask_nodes=2,provider_peak=dict(ram_bytes=1,scratch_bytes=1,vram_bytes=1,output_bytes=1))
    required=render_resources(6000,4000,**args)
    assert admit_render({'resource_estimate':required},6000,4000,**args)==required
    estimate=dict(required);estimate[key]-=1
    with pytest.raises(OperationFailure,match='ADMISSION_EXCEEDED'):
        admit_render({'resource_estimate':estimate},6000,4000,**args)


@pytest.mark.parametrize('bad',[None,{}, {'pixel_count':True}])
def test_admission_rejects_invalid_profile(bad):
    from photo_worker.v2.render_resources import admit_render
    from photo_worker.runtime.executor import OperationFailure
    with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
        admit_render({'resource_estimate':bad},1,1,mask_refs=0,mask_nodes=0,
            provider_peak=dict.fromkeys(['ram_bytes','scratch_bytes','vram_bytes','output_bytes'],0))

import pytest
from photo_worker.operations.layers.resources import layer_resources,layer_tile_bytes,admit_layer
from photo_worker.runtime.executor import OperationFailure


def test_large_canvas_and_mask_dag_budget_counts_resident_maps_and_tiles():
    small=layer_resources(20000,10000,mask_refs=1,mask_nodes=1)
    large=layer_resources(20000,10000,mask_refs=128,mask_nodes=256)
    assert large['ram_bytes']-small['ram_bytes']==4*127*200_000_000+(4*259-256)*512*512
    assert large['scratch_bytes']-small['scratch_bytes']==4*127*200_000_000
    assert large['output_bytes']==24*200_000_000+32*1024*1024
    assert large['vram_bytes']==0 and large['pixel_count']==200_000_000
    assert layer_tile_bytes(257,2,mask_nodes=256,tile_size=256)==4*259*256*2
    assert admit_layer({'resource_estimate':large},20000,10000,mask_refs=128,mask_nodes=256)==large


@pytest.mark.parametrize('field,code',[('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED'),('output_bytes','OUTPUT_ADMISSION_EXCEEDED'),('pixel_count','CONFIG_INVALID'),('vram_bytes','CONFIG_INVALID')])
def test_underestimated_or_mismatched_profile_refused(field,code):
    estimate=layer_resources(2,1,mask_refs=1,mask_nodes=1)
    estimate[field]=1 if field=='vram_bytes' else estimate[field]-1
    with pytest.raises(OperationFailure) as caught:admit_layer({'resource_estimate':estimate},2,1,mask_refs=1,mask_nodes=1)
    assert caught.value.error.code==code


@pytest.mark.parametrize('kwargs',[{'width':True},{'height':0},{'width':200000001},{'mask_refs':0},{'mask_nodes':257},{'tile_size':128}])
def test_invalid_dimensions_and_dag_refused(kwargs):
    values=dict(width=1,height=1,mask_refs=1,mask_nodes=1);values.update(kwargs)
    with pytest.raises(ValueError):layer_resources(**values)


def test_explicit_normalization_reserves_both_parent_mappings():
    baseline=layer_resources(5,7,mask_refs=1,mask_nodes=1)
    normalized=layer_resources(5,7,mask_refs=1,mask_nodes=1,alpha_mode='normalize_uint16')
    for field in ('ram_bytes','scratch_bytes'):
        assert normalized[field]-baseline[field]==8*35+8192
    assert normalized['output_bytes']==baseline['output_bytes']
    profile={'resource_estimate':normalized,'layer_alpha_mode':'normalize_uint16'}
    assert admit_layer(profile,5,7,mask_refs=1,mask_nodes=1)==normalized
    for field,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED')]:
        insufficient={**normalized,field:normalized[field]-1}
        with pytest.raises(OperationFailure) as caught:
            admit_layer({**profile,'resource_estimate':insufficient},5,7,mask_refs=1,mask_nodes=1)
        assert caught.value.error.code==code
    with pytest.raises(OperationFailure) as caught:
        admit_layer({'resource_estimate':baseline,'layer_alpha_mode':'automatic'},5,7,mask_refs=1,mask_nodes=1)
    assert caught.value.error.code=='CONFIG_INVALID'

from copy import deepcopy
import pytest
from photo_worker.operations.metadata.resources import validate_metadata_resources


def profile():
    return {'backend':'cpu','renderer_version':'v2.2','resource_estimate':{
        'ram_bytes':(256<<20)+12*64+4096*4,'scratch_bytes':4096*3+(8<<20),
        'output_bytes':4096+(2<<20),'pixel_count':12,'vram_bytes':0}}


def test_metadata_resource_floor_matches_planner():
    assert validate_metadata_resources(profile(),4,3,4096)==4096+(2<<20)


@pytest.mark.parametrize('field',['ram_bytes','scratch_bytes','output_bytes','pixel_count','vram_bytes'])
def test_metadata_resource_mismatch_refused(field):
    value=deepcopy(profile())
    value['resource_estimate'][field]+=1 if field=='vram_bytes' else -1
    with pytest.raises(ValueError):validate_metadata_resources(value,4,3,4096)


@pytest.mark.parametrize('size',[0,True,2147483649])
def test_metadata_input_bound_before_decode(size):
    with pytest.raises(ValueError):validate_metadata_resources(profile(),4,3,size)

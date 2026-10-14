from dataclasses import replace
from uuid import uuid4
import pytest
from photo_worker.v2.adoption import ArtifactRef,AdoptionInvalid
from photo_worker.v2.render_cache_identity import render_cache_identity
from photo_worker.v2.render_qualification_pin import RenderQualificationPin


def values():
    pin=RenderQualificationPin('a'*64,'b'*64,'cuda','c'*64,'d'*64,'e'*64)
    profile=dict(renderer_version='v2.2',backend='cuda',model_lock_sha=pin.model_lock_sha,
        render_runtime_sha=pin.runtime_lock_sha,icc_lock_sha='f'*64,tile_size=512,precision='float32',
        contracts_sha='1'*64,v2_entries_sha='2'*64)
    return [uuid4(),ArtifactRef(uuid4(),'3'*64),profile,
        (ArtifactRef(uuid4(),'4'*64),ArtifactRef(uuid4(),'5'*64)),pin]


@pytest.mark.parametrize('axis',['tenant','recipe','order','parent','icc','precision','closure','contracts',
                                  'device_profile_sha','dataset_sha','calibration_sha'])
def test_every_content_scope_and_qualification_axis_changes_identity(axis):
    args=values();before=render_cache_identity(*args)
    if axis=='tenant':args[0]=uuid4()
    elif axis=='recipe':args[1]=ArtifactRef(args[1].artifact_id,'6'*64)
    elif axis=='order':args[3]=tuple(reversed(args[3]))
    elif axis=='parent':args[3]=(ArtifactRef(args[3][0].artifact_id,'7'*64),args[3][1])
    elif axis in ('icc','precision','closure','contracts'):
        field={'icc':'icc_lock_sha','precision':'precision','closure':'v2_entries_sha','contracts':'contracts_sha'}[axis]
        args[2][field]='float16' if axis=='precision' else '8'*64
    else:args[4]=replace(args[4],**{axis:'9'*64})
    assert render_cache_identity(*args)!=before


def test_recipe_publication_uuid_and_object_order_do_not_change_content_identity():
    args=values();before=render_cache_identity(*args)
    args[1]=ArtifactRef(uuid4(),args[1].sha256);args[2]=dict(reversed(list(args[2].items())))
    assert render_cache_identity(*args)==before


@pytest.mark.parametrize('kind',['pin','backend','duplicate','nonfinite','oversize','bad_ref'])
def test_invalid_identity_cannot_become_cache_key(kind):
    args=values()
    if kind=='pin':args[4]=None
    elif kind=='backend':args[2]['backend']='cpu'
    elif kind=='duplicate':args[3]=(args[3][0],args[3][0])
    elif kind=='nonfinite':args[2]['cost']=float('nan')
    elif kind=='oversize':args[2]['large']='x'*1048576
    else:args[1]=ArtifactRef(None,'3'*64)
    with pytest.raises(AdoptionInvalid):render_cache_identity(*args)


def test_different_task_transport_manifests_preserve_semantic_identity():
    args=values();args[2].update(v2_entry_count=2,v2_input_manifest={'artifact_id':str(uuid4()),'sha256':'a'*64})
    before=render_cache_identity(*args)
    args[2]['v2_input_manifest']={'artifact_id':str(uuid4()),'sha256':'b'*64}
    assert render_cache_identity(*args)==before
    args[2]['v2_entry_count']=3
    assert render_cache_identity(*args)!=before

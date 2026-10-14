import json
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.portrait_local import PortraitHairParams,PortraitClothingParams
from photo_worker.algorithms.portrait_local import hair_proposal,clothing_proposal
from photo_worker.algorithms.portrait_local_tiles import local_proposal_tiles


@pytest.mark.parametrize('clothing',[False,True])
@pytest.mark.parametrize('shape',[(1,17),(57,83),(260,193)])
def test_proposal_tiles_match_full_frame_including_seams(clothing,shape):
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    body={'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':1,'mask':ref,'protected':ref,'hue_delta_degrees':20,'saturation_factor':1.2,('wrinkle_strength' if clothing else 'detail'):.8}
    model=PortraitClothingParams if clothing else PortraitHairParams
    params=model.model_validate_json(json.dumps(body))
    rgb=np.random.default_rng(9).uniform(.01,4,(*shape,3)).astype(np.float32)
    expected=(clothing_proposal if clothing else hair_proposal)(rgb,20,1.2,.8)
    output=np.empty_like(rgb)
    for (y,y1,x,x1),patch in local_proposal_tiles(rgb,params,tile=31):
        output[y:y1,x:x1]=patch
    np.testing.assert_allclose(output,expected,rtol=2e-6,atol=1e-6)


@pytest.mark.parametrize('clothing',[False,True])
def test_tiled_scratch_preparation_matches_full_frame_encoded_result(tmp_path,clothing):
    from pathlib import Path
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.runtime.scratch import ScratchSpace
    from photo_worker.operations.portrait.prepare_local import prepare_local
    from photo_worker.operations.portrait.local_resources import local_portrait_resources
    from photo_worker.operations.optical.color import to_working_linear,from_working_linear
    from photo_worker.algorithms.portrait_local import compose_local
    data=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_bytes())
    color=WorkingImage.model_validate_json(json.dumps(data[0])).color
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    body={'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':.7,'mask':ref,'protected':ref,'hue_delta_degrees':20,'saturation_factor':1.2,('wrinkle_strength' if clothing else 'detail'):.8}
    model=PortraitClothingParams if clothing else PortraitHairParams
    params=model.model_validate_json(json.dumps(body))
    h,w=531,577
    rgb=np.random.default_rng(19).uniform(.01,1.4,(h,w,3)).astype(np.float32)
    target=np.full((h,w),.8,np.float32);target[:11]=0
    protected=np.zeros((h,w),np.float32);protected[:,505:520]=1
    linear=to_working_linear(rgb,color)
    proposal=(clothing_proposal if clothing else hair_proposal)(linear,20,1.2,.8)
    expected=from_working_linear(compose_local(linear,proposal,target,protected,.7),color)
    unchanged=(target==0)|(protected==1);expected[unchanged]=rgb[unchanged]
    profile={'renderer_version':'v2.2','backend':'cpu','resource_estimate':local_portrait_resources(w,h)}
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,32*1024*1024,min_free_bytes=0) as scratch:
        with prepare_local(params,rgb,target,protected,color,scratch,profile) as files:
            output=np.load(files.rgb)
            np.testing.assert_allclose(output,expected,rtol=3e-6,atol=2e-6)
            np.testing.assert_array_equal(output[unchanged],rgb[unchanged])
            np.testing.assert_array_equal(np.load(files.changed_mask),np.any(output!=rgb,axis=-1).astype(np.float32))
        assert not list(scratch.directory.iterdir())

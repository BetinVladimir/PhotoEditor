import json
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.portrait_local import PortraitClothingParams
from photo_worker.operations.portrait.local_result import stage_local_result
from photo_worker.operations.optical.contracts import ImageResult


class Artifacts:
    def __init__(self):
        self.roles = {}
    def stage(self,path,reservation,role,mime):
        ref = ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
        self.roles[role] = (ref,path)
        return ref
    def stage_contract(self,value,role):
        ref = ArtifactRef(artifact_id=uuid4(),sha256='b'*64)
        self.roles[role] = (ref,value)
        return ref


@pytest.mark.parametrize('strength', [0.,1.])
def test_local_result_source_bypass_and_geometry_preservation(tmp_path,strength):
    data = json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_bytes())
    source = WorkingImage.model_validate_json(json.dumps(data[0]))
    ref = ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    params = PortraitClothingParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref.model_dump(mode='json'),'canvas_hash':canonical_hash(source.canvas)},'strength':strength,'mask':ref.model_dump(mode='json'),'protected':ref.model_dump(mode='json'),'hue_delta_degrees':0,'saturation_factor':1,'wrinkle_strength':1}))
    rgb,changed=tmp_path/'rgb.npy',tmp_path/'changed.npy'
    np.save(rgb,np.zeros((2,2,3),np.float32));np.save(changed,np.zeros((2,2),np.float32))
    files=SimpleNamespace(rgb=rgb,changed_mask=changed,reservation=SimpleNamespace(validate=lambda:None),review_required=strength>0)
    artifacts=Artifacts()
    staged=stage_local_result(artifacts,params,source,files)
    result=artifacts.roles['document'][1]
    assert isinstance(result,ImageResult) and result.generated_mask is None
    assert staged.document == artifacts.roles['document'][0]
    assert len(staged.outputs)==6
    assert staged.bytes_written>0
    image=artifacts.roles['working_image'][1]
    assert image.canvas==source.canvas and image.transform_chain==source.transform_chain
    assert image.alpha==source.alpha and image.color==source.color
    assert image.pixels==artifacts.roles['pixels'][0]
    assert result.working_image==artifacts.roles['working_image'][0]
    operations=json.loads((Path(__file__).resolve().parents[2]/'docs/implementation-v2/contracts/operations.json').read_text())['operations']
    policy=next(op for op in operations if op['id']=='portrait.clothing')['role_policies']
    assert all(role in artifacts.roles for role, rule in policy.items() if rule['required'])
    report=artifacts.roles['report'][1]
    assert report.status==('review' if strength else 'pass')
    assert report.operation=='portrait.clothing'
    assert artifacts.roles['mask'][1].canvas==source.canvas
    bad=params.model_copy(update={'base':params.base.model_copy(update={'canvas_hash':'d'*64})})
    refused=Artifacts()
    with pytest.raises(ValueError,match='PORTRAIT_SOURCE_MISMATCH'):
        stage_local_result(refused,bad,source,files)
    assert not refused.roles

    files.review_required=not files.review_required
    refused=Artifacts()
    with pytest.raises(ValueError,match='PORTRAIT_REVIEW_MISMATCH'):
        stage_local_result(refused,params,source,files)
    assert not refused.roles


@pytest.mark.parametrize('operation', ['hair','clothing'])
@pytest.mark.parametrize('encoding', ['linear','gamma24'])
@pytest.mark.parametrize('strength', [0.,1.])
def test_prepare_and_stage_preserve_alpha_and_guard_bits(tmp_path,operation,encoding,strength):
    from photo_worker.contracts.portrait_local import PortraitHairParams
    from photo_worker.operations.portrait.prepare_local import prepare_local
    from photo_worker.operations.portrait.local_resources import local_portrait_resources
    from photo_worker.runtime.scratch import ScratchSpace
    fixture=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_bytes())[0]
    source=WorkingImage.model_validate_json(json.dumps(fixture))
    alpha=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    color=source.color.model_copy(update={'encoding':encoding,'profile_id':'linear_srgb_v1' if encoding=='linear' else 'work_rec2020_gamma24_v1'})
    source=source.model_copy(update={'alpha':alpha,'color':color})
    ref=ArtifactRef(artifact_id=uuid4(),sha256='c'*64)
    body={'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref.model_dump(mode='json'),'canvas_hash':canonical_hash(source.canvas)},'strength':strength,'mask':ref.model_dump(mode='json'),'protected':ref.model_dump(mode='json'),'hue_delta_degrees':20,'saturation_factor':1.2}
    if operation=='hair':body['detail']=.8;model=PortraitHairParams
    else:body['wrinkle_strength']=.8;model=PortraitClothingParams
    params=model.model_validate_json(json.dumps(body))
    h,w=source.canvas.height,source.canvas.width
    rgb=np.random.default_rng(781).uniform(.01,1.2,(h,w,3)).astype(np.float32)
    rgb[0,0,0]=np.float32(-0.)
    target=np.ones((h,w),np.float32);target[:,:73]=0
    guard=np.zeros((h,w),np.float32);guard[39:111,121:209]=1
    protected=(target==0)|(guard==1)
    resources=local_portrait_resources(w,h)
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    artifacts=Artifacts()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,resources['scratch_bytes'],min_free_bytes=0) as scratch:
        with prepare_local(params,rgb,target,guard,color,scratch,{'renderer_version':'v2.2','backend':'cpu','resource_estimate':resources}) as files:
            output=np.load(files.rgb,allow_pickle=False)
            changed=np.load(files.changed_mask,allow_pickle=False)
            np.testing.assert_array_equal(output[protected].view(np.uint32),rgb[protected].view(np.uint32))
            assert np.all(changed[protected]==0)
            if strength==0:
                np.testing.assert_array_equal(output.view(np.uint32),rgb.view(np.uint32))
                assert np.all(changed==0)
            else:
                assert np.any(output[~protected]!=rgb[~protected])
            staged=stage_local_result(artifacts,params,source,files)
            assert staged.bytes_written>0
            image=artifacts.roles['working_image'][1]
            assert image.alpha==alpha and image.color==color
            assert image.canvas==source.canvas and image.transform_chain==source.transform_chain
            assert artifacts.roles['report'][1].outside_mask_max_error==0
            assert artifacts.roles['document'][1].generated_mask is None

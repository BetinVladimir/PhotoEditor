import json
from pathlib import Path
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.operations.portrait.prepare_relight import prepare_relight
from photo_worker.operations.portrait.local_resources import local_portrait_resources
from photo_worker.operations.optical.color import to_working_linear,from_working_linear
from photo_worker.algorithms.relight import build_light_field,relight_proposal
from photo_worker.algorithms.portrait_local import compose_local


@pytest.mark.parametrize('strength',[0.,.7])
def test_relight_scratch_matches_full_reference_and_preserves_pixels(tmp_path,strength):
    h,w=267,519
    ref={'artifact_id':str(uuid4()),'sha256':'a'*64}
    params=ImageRelightParams.model_validate_json(json.dumps({'schema_version':1,'base':{'asset_version_id':str(uuid4()),'source':ref,'canvas_hash':'b'*64},'strength':strength,'depth':ref,'subject':ref,'protected':ref,'lights':[{'x':.5,'y':.5,'radius':.3,'ev':.8}],'calibration':ref}))
    images=json.loads((Path(__file__).resolve().parents[2]/'backend/internal/review/testdata/working-images-python.json').read_text())
    color=WorkingImage.model_validate_json(json.dumps(images[0])).color
    rgb=np.random.default_rng(20).uniform(.01,1.4,(h,w,3)).astype(np.float32)
    depth=np.linspace(0,1,h*w,dtype=np.float32).reshape(h,w)
    invalid=np.zeros((h,w),bool);invalid[:5]=True;depth[:5]=np.nan
    subject=np.full((h,w),.8,np.float32);subject[:,0:10]=0
    protected=np.zeros((h,w),np.float32);protected[:,250:260]=1
    protected[:,260:270]=.25
    protected[:,270:280]=.99
    protected[:,280:290]=np.nextafter(np.float32(0),np.float32(1))
    field=build_light_field(depth,invalid,params.lights,.1)
    linear=to_working_linear(rgb,color)
    expected=from_working_linear(compose_local(linear,relight_proposal(linear,field),subject,(protected>0).astype(np.float32),strength),color)
    untouched=(subject==0)|(protected>0)|(field==0)
    if strength==0:untouched[:]=True
    expected[untouched]=rgb[untouched]
    profile={'renderer_version':'v2.2','backend':'cpu','resource_estimate':local_portrait_resources(w,h)}
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,profile['resource_estimate']['scratch_bytes'],min_free_bytes=0) as scratch:
        with prepare_relight(params,rgb,depth,invalid,subject,protected,color,scratch,profile,depth_sigma=.1) as files:
            actual=np.load(files.rgb);changed=np.load(files.changed_mask)
            np.testing.assert_allclose(actual,expected,rtol=2e-6,atol=1e-6)
            np.testing.assert_array_equal(actual[untouched],rgb[untouched])
            np.testing.assert_array_equal(changed,np.any(actual!=rgb,axis=-1).astype(np.float32))
            assert files.review_required
        assert not list(scratch.directory.iterdir())
        calls=0
        def cancel():
            nonlocal calls
            calls+=1
            if calls==16:raise RuntimeError('cancelled')
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_relight(params,rgb,depth,invalid,subject,protected,color,scratch,profile,depth_sigma=.1,check_active=cancel):
                pytest.fail('cancelled preparation yielded')
        assert not list(scratch.directory.iterdir())

        with pytest.raises(ValueError,match='DEPTH_UNAVAILABLE'):
            with prepare_relight(params,rgb,depth,np.ones((h,w),bool),subject,protected,color,scratch,profile,depth_sigma=.1):
                pytest.fail('invalid depth yielded')
        assert not list(scratch.directory.iterdir())
        from photo_worker.runtime.executor import OperationFailure
        low=dict(profile,resource_estimate=dict(profile['resource_estimate'],ram_bytes=1))
        with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
            with prepare_relight(params,rgb,depth,invalid,subject,protected,color,scratch,low,depth_sigma=.1):
                pytest.fail('insufficient RAM yielded')
        assert not list(scratch.directory.iterdir())

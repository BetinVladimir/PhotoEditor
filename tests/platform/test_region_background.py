from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.style import Background
from photo_worker.contracts.background import ComposeParams
from photo_worker.operations.region.background import background_entry,prepare_region_background
from photo_worker.operations.background.prepare import prepare_background
from photo_worker.operations.region.geometry_native import _alpha16
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.scratch import ScratchSpace
from test_image_color_adapter import Artifacts


def fixture(tmp_path,mode):
    a=Artifacts(tmp_path,False)
    values=np.full(a.rgb.shape[:2],.8,np.float32)
    mask=MaskRef(artifact=a.array(tmp_path,'subject',values),canvas=a.image.canvas,
        transform_chain=a.image.transform_chain,selector='subject',confidence=.9,protected=False,model_lock_id=None)
    background=Background(mode=mode,asset_id=None,solid_rgb=[.2,.3,.4] if mode=='solid' else None,
        blur_strength=.5,protect_subject=True)
    effective=SimpleNamespace(strength=.5,extensions=SimpleNamespace(background=background))
    params=ComposeParams(image=a.image_ref,subject=mask,mode=mode,solid_rgb=background.solid_rgb)
    target=a.image.model_copy(update={'alpha':mask.artifact if mode=='transparent' else None})
    stage=SimpleNamespace(operation='background.compose',background_asset=None)
    verified=[(stage,params.model_dump(mode='json'),a.image,target,{})]
    a.require_declared_input=lambda ref:None
    return a,effective,params,values,verified


@pytest.mark.parametrize('mode',['transparent','solid'])
def test_region_background_uses_native_apply_pixels_and_alpha(tmp_path,mode):
    a,effective,params,subject,verified=fixture(tmp_path,mode)
    alpha=np.full(a.rgb.shape[:2],10000,np.float32)/65535
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,20_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with prepare_background(a.rgb,subject,_alpha16(alpha),None,params,scratch) as native:
            with read_array(native.rgb,a.rgb.shape,'float32',lambda:None,unit_interval=True) as rgb:expected=rgb.copy()
            if native.alpha is not None:
                with read_array(native.alpha,a.rgb.shape[:2],'uint16',lambda:None) as alpha_pixels:expected_alpha=alpha_pixels.astype(np.float32)/65535
            else:expected_alpha=None
        with prepare_region_background(a,effective,verified,a.rgb,alpha,a.image) as rendered:
            np.testing.assert_array_equal(rendered.rgb.view(np.uint32),expected.view(np.uint32))
            if expected_alpha is None:assert rendered.alpha is None
            else:np.testing.assert_array_equal(rendered.alpha.view(np.uint32),expected_alpha.view(np.uint32))
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('field,value',[('shadow_opacity',.1),('position_x',.6),('asset_scale',2)])
def test_job_override_controls_are_not_inferred_from_base_style(tmp_path,field,value):
    _,effective,_,_,verified=fixture(tmp_path,'solid')
    verified[0][1][field]=value
    if field=='shadow_opacity':verified[0][1]['shadow_blur_fraction']=.01
    with pytest.raises(ValueError,match='POLICY_MISMATCH'):background_entry(effective,verified)


def test_missing_background_stage_cannot_silently_return_source(tmp_path):
    _,effective,_,_,_=fixture(tmp_path,'transparent')
    with pytest.raises(ValueError,match='COVERAGE_MISMATCH'):background_entry(effective,[])


def test_native_region_depth_blur_matches_shared_pipeline_without_model_substitution(tmp_path):
    from photo_worker.contracts.background import BlurParams
    from photo_worker.contracts.images import RelativeDepth
    from photo_worker.operations.background.prepare_blur import prepare_blur
    a,effective,old,subject,_=fixture(tmp_path,'solid')
    effective.extensions.background=Background(mode='blur',asset_id=None,blur_strength=.5,protect_subject=True)
    depth=np.linspace(0,1,subject.size,dtype=np.float32).reshape(subject.shape)
    depth_ref=RelativeDepth(artifact=a.array(tmp_path,'depth',depth),float32_shape=list(depth.shape),invalid_mask=None,
        canvas=a.image.canvas,transform_chain=a.image.transform_chain,normalization='minmax_valid')
    params=BlurParams(image=a.image_ref,subject=old.subject,depth=depth_ref,strength=.25)
    verified=[(SimpleNamespace(operation='background.blur'),params.model_dump(mode='json'),a.image,a.image,{})]
    alpha=np.full(subject.shape,10000,np.float32)/65535
    root=tmp_path/'blur-scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,20_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with prepare_blur(a.rgb,subject,depth,scratch,strength=.25) as native:
            with read_array(native.rgb,a.rgb.shape,'float32',lambda:None,unit_interval=True) as pixels:expected=pixels.copy()
        with prepare_region_background(a,effective,verified,a.rgb,alpha,a.image) as actual:
            np.testing.assert_array_equal(actual.rgb.view(np.uint32),expected.view(np.uint32))
            assert actual.alpha is alpha
        assert not list(scratch.directory.iterdir())


def test_native_region_portable_asset_composition_uses_exact_decoded_pixels(tmp_path):
    a,effective,old,subject,_=fixture(tmp_path,'solid')
    effective.extensions.background=Background(mode='asset',asset_id='portable-background',blur_strength=.5,protect_subject=True)
    background=np.full_like(a.rgb,[.2,.4,.6]);background_ref=a.array(tmp_path,'decoded-background',background)
    image=a.image.model_copy(update={'pixels':background_ref,'alpha':None})
    image_ref=a.add(image)
    params=ComposeParams(image=a.image_ref,subject=old.subject,mode='asset',background=image_ref)
    stage=SimpleNamespace(operation='background.compose',background_asset=SimpleNamespace(portable_asset_id='portable-background'))
    verified=[(stage,params.model_dump(mode='json'),a.image,a.image.model_copy(update={'alpha':None}),{'background_image':image})]
    root=tmp_path/'asset-scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,20_000_000,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with prepare_background(a.rgb,subject,None,background,params,scratch) as native:
            with read_array(native.rgb,a.rgb.shape,'float32',lambda:None,unit_interval=True) as pixels:expected=pixels.copy()
        with prepare_region_background(a,effective,verified,a.rgb,None,a.image) as actual:
            np.testing.assert_array_equal(actual.rgb.view(np.uint32),expected.view(np.uint32))
            assert actual.alpha is None
        assert not list(scratch.directory.iterdir())

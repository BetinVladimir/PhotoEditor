from dataclasses import replace
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.background import BlurParams
from photo_worker.v2.render_background import prepare_blur_stage
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_effects import effect_review_required, unpack_render_base
from photo_worker.runtime.scratch import ScratchSpace
from test_background_blur import Artifacts


def test_render_blur_retains_alpha_generated_and_review(tmp_path):
    a=Artifacts(tmp_path/'source',confidence=.8)
    alpha=np.full(a.rgb.shape[:2],.4321,np.float32)
    generated=np.zeros_like(alpha)
    current=AdjustmentPixels(a.rgb,alpha,generated)
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:
        with prepare_blur_stage(SimpleNamespace(scratch=scratch),a.image,current,
                a.params(BlurParams),a.subject_array,a.depth_array) as stage:
            pixels,evidence=unpack_render_base(stage)
            assert pixels.alpha is alpha and pixels.generated is generated
            assert not pixels.rgb.flags.writeable
            assert evidence is stage and effect_review_required(stage)
            assert 0<=stage.subject_depth<=1
            assert stage.maximum_radius>=0
            # A provider flag cannot suppress the measured mask confidence.
            assert effect_review_required(replace(stage,review_required=False))
            with pytest.raises(ValueError,match='RENDER_EFFECT_EVIDENCE_INVALID'):
                effect_review_required(replace(stage,subject_depth=float('nan')))


def test_authored_blur_strength_is_materialized_without_color_scaling(tmp_path):
    from photo_worker.v2.render_background import render_blur_params
    a=Artifacts(tmp_path/'source')
    background=SimpleNamespace(mode='blur',blur_strength=.63)
    style=SimpleNamespace(effective=SimpleNamespace(strength=0,
        extensions=SimpleNamespace(background=background)))
    params=render_blur_params(style,a.image_ref,a.subject,a.depth,tile_size=256)
    assert params.strength==.63 and params.tile==256
    assert params.image==a.image_ref and params.subject==a.subject and params.depth==a.depth
    background.mode='generate'
    with pytest.raises(ValueError,match='RENDER_BLUR_STYLE_INVALID'):
        render_blur_params(style,a.image_ref,a.subject,a.depth)


@pytest.mark.parametrize('mode',['blur','solid','transparent','asset'])
def test_ordered_color_and_blur_matches_separate_native_stages(tmp_path,mode):
    from pathlib import Path
    from photo_worker.contracts.style import StyleV2
    from photo_worker.v2.render_style import PreparedRenderStyle
    from photo_worker.operations.session.contracts import CorrectionVector
    from photo_worker.v2.render_portrait_background import prepare_portrait_background_base,portrait_background_peak
    from photo_worker.v2.render_color import prepare_color_base
    from photo_worker.v2.render_background import render_blur_params
    from photo_worker.v2.render_resources import render_resources
    a=Artifacts(tmp_path/'source',confidence=.8)
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    value.quality.status='needs_review'
    data=value.model_dump(mode='python',by_alias=True)
    data['extensions']['background']=dict(mode=mode,asset_id="studio" if mode=="asset" else None,blur_strength=.63,protect_subject=True,solid_rgb=[.2,.3,.4] if mode=='solid' else None)
    effective=StyleV2.model_validate(data)
    style=PreparedRenderStyle(value,effective,effective,(),CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0))
    state=SimpleNamespace(source=a.image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(protected_mask=None,source=SimpleNamespace(source=a.image_ref))))
    original=AdjustmentPixels(a.rgb,None)
    from photo_worker.v2.render_background import render_compose_params,prepare_background_stage
    params=(render_blur_params(style,a.image_ref,a.subject,a.depth,tile_size=256) if mode=='blur' else
        render_compose_params(style,a.image_ref,a.subject,tile_size=256) if mode!='asset' else None)
    normalized=None
    if mode=='asset':
        from photo_worker.contracts.background import ComposeParams
        from photo_worker.v2.render_background_binding import FrozenBackgroundAsset
        from photo_worker.v2.render_background_decode import NormalizedBackground
        from photo_worker.operations.asset.decode import WorkingImage as DecodedImage
        pin=FrozenBackgroundAsset(schema_version=1,portable_asset_id='studio',style_version_id=uuid4(),
            style=a.image_ref,source=a.image_ref,expected_bytes=100,media_type='image/png',suffix='.png',
            assume_profile='srgb_v1',work_icc_sha=a.image.color.icc_sha256)
        state.recipe.background_asset=pin
        state.recipe.recipe.base_style=pin.style
        bg=np.flip(a.rgb,axis=1).copy();bg.flags.writeable=False
        normalized=NormalizedBackground(DecodedImage(Path('owned.npy'),None,bg.shape[1],bg.shape[0],
            'work_rec2020_gamma24_v1',pin.work_icc_sha,pin.source.sha256,True),bg)
        params=ComposeParams(image=a.image_ref,subject=a.subject,mode='asset',background=pin.source,tile=256)
    peak=portrait_background_peak(state,style,(),normalized_background=normalized,tile_size=256)
    profile={'resource_estimate':render_resources(a.image.canvas.width,a.image.canvas.height,
        mask_refs=0,mask_nodes=0,provider_peak=peak,tile_size=256)}
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4_000_000,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(scratch=scratch)
        with prepare_color_base(artifacts,state,replace(style,effective=value),original,
                person_reliable=False,tile_size=256) as colored:
            stage=(prepare_blur_stage(artifacts,a.image,colored,params,a.subject_array,a.depth_array) if mode=='blur' else
                prepare_background_stage(artifacts,a.image,colored,params,a.subject_array,
                    background_rgb=None if normalized is None else normalized.rgb))
            with stage as separate:
                expected=separate.pixels.rgb.copy()
                expected_alpha=None if separate.pixels.alpha is None else separate.pixels.alpha.copy()
        with prepare_portrait_background_base(artifacts,state,style,original,SimpleNamespace(protection=None),
                faces=(),person_reliable=False,execution_profile=profile,blur_params=params if mode=='blur' else None,
                compose_params=params if mode!='blur' else None,subject=a.subject_array,
                depth=a.depth_array if mode=='blur' else None,normalized_background=normalized,tile_size=256) as combined:
            np.testing.assert_array_equal(combined.pixels.rgb,expected)
            if expected_alpha is None: assert combined.pixels.alpha is None
            else: np.testing.assert_array_equal(combined.pixels.alpha,expected_alpha)
            assert len(combined.effects)==2 and effect_review_required(combined)
        wrong=params.model_copy(update={'image':params.image.model_copy(update={'artifact_id':uuid4()})})
        with pytest.raises(ValueError,match='AUTHORED_PARAMS_CHANGED'):
            with prepare_portrait_background_base(artifacts,state,style,original,SimpleNamespace(protection=None),
                    faces=(),person_reliable=False,execution_profile=profile,
                    blur_params=wrong if mode=='blur' else None,
                    compose_params=wrong if mode!='blur' else None,subject=a.subject_array,
                    depth=a.depth_array if mode=='blur' else None,normalized_background=normalized,tile_size=256):
                pytest.fail('changed source ref accepted')
        if normalized is not None:
            from photo_worker.v2.render_portrait_background import _bound_background
            for wrong in (replace(normalized,image=replace(normalized.image,source_sha256='f'*64)),
                          replace(normalized,image=replace(normalized.image,profile_sha256='f'*64)),
                          replace(normalized,rgb=normalized.rgb.copy())):
                with pytest.raises(ValueError,match='RENDER_BACKGROUND_PIN_CHANGED'):
                    _bound_background(state,style,wrong)
            effective.extensions.background.asset_id='other'
            with pytest.raises(ValueError,match='RENDER_BACKGROUND_PIN_CHANGED'):
                _bound_background(state,style,normalized)


@pytest.mark.parametrize('mode',['transparent','solid'])
def test_render_compose_uses_full_float_alpha_precision(tmp_path,mode):
    from test_background_compose import Artifacts as ComposeArtifacts
    from photo_worker.contracts.background import ComposeParams
    from photo_worker.v2.render_background import prepare_background_stage
    from photo_worker.algorithms.background import solid,transparent
    root=tmp_path/'source';root.mkdir()
    a=ComposeArtifacts(root,mode,.8)
    alpha=np.full(a.rgb.shape[:2],.43212345,np.float32)
    current=AdjustmentPixels(a.rgb,alpha,np.zeros_like(alpha))
    params=a.params(ComposeParams)
    combined=a.subject_array*alpha
    scratch_root=tmp_path/'scratch';scratch_root.mkdir()
    with ScratchSpace(scratch_root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:
        with prepare_background_stage(SimpleNamespace(scratch=scratch),a.image,current,params,
                a.subject_array) as stage:
            assert effect_review_required(stage)
            assert stage.pixels.generated is current.generated
            if mode=='transparent':
                np.testing.assert_array_equal(stage.pixels.alpha,combined)
                np.testing.assert_array_equal(stage.pixels.rgb,transparent(a.rgb,combined))
                assert np.any(combined*65535!=np.rint(combined*65535))
            else:
                assert stage.pixels.alpha is None
                np.testing.assert_array_equal(stage.pixels.rgb,solid(a.rgb,combined,params.solid_rgb))


def test_background_asset_loader_owns_metadata_and_readonly_pixels(tmp_path):
    from test_background_compose import Artifacts as ComposeArtifacts
    from photo_worker.v2.render_background import load_background_asset
    root=tmp_path/'source';root.mkdir()
    a=ComposeArtifacts(root,'solid',source_alpha=False)
    with load_background_asset(a,a.image,a.image_ref) as (image,rgb):
        assert image==a.image and image is not a.image
        assert not rgb.flags.writeable
        np.testing.assert_array_equal(rgb,a.rgb)
    bad=a.image.model_copy(update={'alpha':a.image.pixels})
    with pytest.raises(ValueError,match='RENDER_BACKGROUND_ASSET_INVALID'):
        with load_background_asset(a,a.image,a.add(bad)):
            pytest.fail('alpha background accepted')


@pytest.mark.parametrize('cancel_phase',[None,'normalize','compose','yield','mutate'])
def test_frozen_import_normalizes_and_renders_inside_one_owner(tmp_path,cancel_phase):
    from contextlib import contextmanager
    from hashlib import sha256
    from pathlib import Path
    from PIL import Image
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.contracts.base import ArtifactRef,canonical_hash
    from photo_worker.contracts.style import StyleV2
    from photo_worker.v2.render_style import PreparedRenderStyle
    from photo_worker.v2.render_inputs import FrozenRenderInputs
    from photo_worker.v2.render_recipe import FrozenRenderRecipe,EditingRecipeV3
    from photo_worker.v2.render_portrait_background import prepare_frozen_asset_background_base
    from photo_worker.operations.session.contracts import CorrectionVector
    a=Artifacts(tmp_path/'source')
    registry=ICCRegistry(Path(__file__).parents[2]/'resources/icc')
    icc=registry.resolve('work_rec2020_gamma24_v1').sha256
    source=a.image.model_copy(update={'color':a.image.color.model_copy(update={'icc_sha256':icc})})
    path=tmp_path/'background.png';Image.new('RGB',(19,11),(80,120,180)).save(path,icc_profile=registry.resolve('srgb_v1').data)
    imported=ArtifactRef(artifact_id=uuid4(),sha256=sha256(path.read_bytes()).hexdigest())
    style_ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64)
    delta=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
    binding=dict(asset_version_id=uuid4(),source=a.image_ref,canvas_hash=canonical_hash(source.canvas))
    recipe=dict(schema_version=3,source=binding,base_style=style_ref,session_corrections=None,
        learning_version_id=None,overrides=[],layer_stack=None,renderer_version='v2.2',parameter_delta=delta)
    recipe=EditingRecipeV3.model_validate(recipe)
    frozen=FrozenRenderRecipe.model_validate(dict(schema_version=1,recipe=recipe,
        recipe_sha256=canonical_hash(recipe),learning=None,
        scalars=dict(schema_version=1,source=binding,base_style=style_ref,effective_delta=delta,
            renderer_version='v2.2',icc_lock_sha=icc,model_lock_sha='d'*64),
        background_asset=dict(schema_version=1,portable_asset_id='studio',style_version_id=uuid4(),
            style=style_ref,source=imported,expected_bytes=path.stat().st_size,media_type='image/png',
            suffix='.png',assume_profile=None,work_icc_sha=icc)))
    inputs=FrozenRenderInputs(frozen,source,None,None)
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    data=value.model_dump(mode='python',by_alias=True)
    data['quality']['status']='needs_review'
    data['extensions']['background']=dict(mode='asset',asset_id='studio',blur_strength=0,protect_subject=True)
    effective=StyleV2.model_validate(data);style=PreparedRenderStyle(value,effective,effective,(),delta)
    calls=[]
    @contextmanager
    def input(ref,*,suffix):
        assert ref==imported and suffix=='.png';calls.append('open')
        try:yield SimpleNamespace(path=path)
        finally:calls.append('close')
    root=tmp_path/'scratch';root.mkdir()
    cancelled=False
    from photo_worker.runtime.executor import OperationFailure
    def check_active():
        if cancelled or (cancel_phase=='normalize' and any(root.rglob('*normalized*.npy*'))) or (cancel_phase=='compose' and any(root.rglob('background-rgb*.npy'))):
            raise OperationFailure('CANCELLED')
    profile={'resource_estimate':dict(ram_bytes=8*1024**3,scratch_bytes=8*1024**3,output_bytes=1024**3,
        vram_bytes=0,pixel_count=source.canvas.width*source.canvas.height)}
    with ScratchSpace(root,uuid4(),uuid4(),1,4_000_000,min_free_bytes=0) as scratch:
        from photo_worker.v2.render_native_base import NativePortraitBackgroundFactory,NativeBaseEvidence
        @contextmanager
        def evidence(state,prepared,original,masks):
            calls.append('analysis-open')
            if cancel_phase=='mutate':
                state.recipe.background_asset.portable_asset_id='other'
                prepared.effective.extensions.background.asset_id='other'
            try:yield NativeBaseEvidence((),False,subject_ref=a.subject,subject=a.subject_array)
            finally:calls.append('analysis-close')
        factory=NativePortraitBackgroundFactory(registry,
            evidence_factory=lambda *args:evidence,
            retained_resources=lambda *args:dict(ram_bytes=a.rgb.nbytes,scratch_bytes=0))
        provider=factory(SimpleNamespace(input=input,scratch=scratch),inputs,
            dict(profile,tile_size=256),check_active)
        if cancel_phase in ('normalize','compose','yield'):
            with pytest.raises(OperationFailure,match='CANCELLED'):
                with provider(inputs,style,AdjustmentPixels(a.rgb,None),SimpleNamespace(protection=None)) as result:
                    assert cancel_phase=='yield'
                    cancelled=True
            assert calls==['analysis-open','open','close','analysis-close']
            assert not list(root.rglob('*.npy*'))
            return
        with provider(inputs,style,AdjustmentPixels(a.rgb,None),SimpleNamespace(protection=None)) as result:
            assert calls==['analysis-open','open']
            assert result.pixels.rgb.shape==a.rgb.shape and not result.pixels.rgb.flags.writeable
            assert result.pixels.alpha is None and np.isfinite(result.pixels.rgb).all()
            assert len(result.effects)==2
        assert calls==['analysis-open','open','close','analysis-close']
        assert frozen.background_asset.portable_asset_id=='studio'
        assert effective.extensions.background.asset_id=='studio'
        assert not list(root.rglob('*.npy'))
        effective.extensions.background.mode='generate'
        with pytest.raises(ValueError,match='RENDER_COMPLETE_PROVIDER_REQUIRED'):
            with provider(inputs,style,AdjustmentPixels(a.rgb,None),SimpleNamespace(protection=None)):
                pytest.fail('unsupported authored generation omitted')
        assert calls==['analysis-open','open','close','analysis-close']


def test_pinned_background_arrays_owned_and_admitted(tmp_path):
    from photo_worker.v2.render_native_base import load_pinned_background_arrays
    from photo_worker.runtime.executor import OperationFailure
    a=Artifacts(tmp_path/'source')
    state=SimpleNamespace(source=a.image)
    subject_document=a.add(a.subject);depth_document=a.add(a.depth)
    budget={'resource_estimate':dict(ram_bytes=100_000_000,scratch_bytes=1_000_000)}
    retained=dict(ram_bytes=1000,scratch_bytes=1000)
    with load_pinned_background_arrays(a,state,subject_document,depth_document,
            execution_profile=budget,retained_resources=retained) as (subject,alpha,depth,distances):
        assert subject==a.subject and subject is not a.subject
        assert depth==a.depth and depth is not a.depth
        assert not alpha.flags.writeable and not distances.flags.writeable
        np.testing.assert_array_equal(alpha,a.subject_array)
        np.testing.assert_array_equal(distances,a.depth_array)
    for bad_subject,bad_depth,code in (
            (a.add(a.subject.model_copy(update={'protected':True})),depth_document,'SUBJECT_INVALID'),
            (subject_document,a.add(a.depth.model_copy(update={'invalid_mask':a.depth.artifact})),'DEPTH_INVALID')):
        with pytest.raises(ValueError,match=code):
            with load_pinned_background_arrays(a,state,bad_subject,bad_depth,
                    execution_profile=budget,retained_resources=retained):pytest.fail('invalid analysis accepted')
    def forbidden(*args,**kwargs):pytest.fail('analysis IO before admission')
    with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
        with load_pinned_background_arrays(SimpleNamespace(contract=forbidden,input=forbidden),state,subject_document,depth_document,
                execution_profile={'resource_estimate':dict(ram_bytes=1000,scratch_bytes=1_000_000)},
                retained_resources=retained):pytest.fail('unadmitted mask maps opened')


def test_background_discovery_is_typed_same_canvas_and_unambiguous(tmp_path):
    from photo_worker.v2.render_native_base import discover_pinned_background_refs
    a=Artifacts(tmp_path/'source')
    subject=a.add(a.subject);depth=a.add(a.depth)
    protected=a.add(a.subject.model_copy(update={'protected':True,'selector':'manual'}))
    refs={'MaskRef':(protected,subject),'RelativeDepth':(depth,)}
    artifacts=SimpleNamespace(contract=a.contract,declared_contract_refs=lambda kind,maximum:refs[kind])
    options=dict(execution_profile={'resource_estimate':dict(ram_bytes=100_000_000,scratch_bytes=0)},
        retained_resources=dict(ram_bytes=0,scratch_bytes=0))
    assert discover_pinned_background_refs(artifacts,a.image,require_depth=True,**options)==(subject,depth)
    assert discover_pinned_background_refs(artifacts,a.image,require_depth=False,**options)==(subject,None)
    second=a.add(a.subject.model_copy(update={'selector':'person'}))
    refs['MaskRef']=(subject,second)
    with pytest.raises(ValueError,match='SUBJECT_SELECTION_INVALID'):
        discover_pinned_background_refs(artifacts,a.image,require_depth=True,**options)
    refs['MaskRef']=(protected,)
    with pytest.raises(ValueError,match='SUBJECT_SELECTION_INVALID'):
        discover_pinned_background_refs(artifacts,a.image,require_depth=False,**options)
    refs['MaskRef']=(subject,);refs['RelativeDepth']=()
    with pytest.raises(ValueError,match='DEPTH_SELECTION_INVALID'):
        discover_pinned_background_refs(artifacts,a.image,require_depth=True,**options)


def test_native_factory_admits_retained_cost_before_evidence_factory():
    from photo_worker.v2.render_native_base import NativePortraitBackgroundFactory
    from photo_worker.runtime.executor import OperationFailure
    calls=[]
    factory=NativePortraitBackgroundFactory(object(),
        evidence_factory=lambda *args:calls.append('evidence'),
        retained_resources=lambda *args:dict(ram_bytes=100,scratch_bytes=100))
    profile=dict(tile_size=256,resource_estimate=dict(ram_bytes=99,scratch_bytes=100))
    with pytest.raises(OperationFailure,match='RAM_ADMISSION_EXCEEDED'):
        factory(object(),object(),profile,lambda:None)
    assert calls==[]


def test_native_factory_keeps_task_profile_and_retained_cost_owned():
    from photo_worker.v2.render_native_base import NativePortraitBackgroundFactory
    retained=dict(ram_bytes=10,scratch_bytes=20)
    profile=dict(tile_size=256,resource_estimate=dict(ram_bytes=100,scratch_bytes=100))
    def loader_factory(artifacts,inputs,costs,active):
        costs['resource_estimate']['ram_bytes']=0
        return lambda *args:None
    factory=NativePortraitBackgroundFactory(object(),evidence_factory=loader_factory,
        retained_resources=lambda *args:retained)
    provider=factory(object(),object(),profile,lambda:None)
    retained['ram_bytes']=0;profile['resource_estimate']['ram_bytes']=0
    assert provider.retained_resources['ram_bytes']==10
    assert provider.execution_profile['resource_estimate']['ram_bytes']==100


def test_native_denoise_provider_owns_costs_and_immutable_configuration():
    from dataclasses import FrozenInstanceError
    from photo_worker.v2.render_native_base import NativeDenoiseProvider
    peak=dict(ram_bytes=100,scratch_bytes=200,vram_bytes=300,output_bytes=0)
    provider=NativeDenoiseProvider(object(),backend='cuda',provider_peak=peak,verify_runtime=lambda:None)
    peak['vram_bytes']=0
    copied=provider.peak();copied['ram_bytes']=0
    assert provider.peak()==dict(ram_bytes=100,scratch_bytes=200,vram_bytes=300,output_bytes=0)
    with pytest.raises(FrozenInstanceError):provider.backend='cpu'
    with pytest.raises(FrozenInstanceError):provider._peak=()


def test_native_denoise_backend_mismatch_rejects_before_factory_io():
    from photo_worker.v2.render_native_base import NativeDenoiseProvider,NativePortraitBackgroundFactory
    def forbidden(*args):raise AssertionError('Evidence/resource factory before backend check')
    provider=NativeDenoiseProvider(object(),backend='cuda',provider_peak=dict(
        ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0),verify_runtime=lambda:None)
    factory=NativePortraitBackgroundFactory(object(),evidence_factory=forbidden,
        retained_resources=forbidden,denoise_provider=provider)
    with pytest.raises(ValueError,match='CONFIG_INVALID'):
        factory(object(),object(),dict(tile_size=256,backend='cpu'),lambda:None)

from dataclasses import replace
from types import SimpleNamespace
from uuid import uuid4
import numpy as np
import pytest
from photo_worker.contracts.base import ArtifactRef
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.contracts.style import StyleV2
from pathlib import Path
from photo_worker.v2.render_style import PreparedRenderStyle
from photo_worker.operations.session.contracts import CorrectionVector
from photo_worker.v2.render_adjustments import AdjustmentPixels
from photo_worker.v2.render_color import prepare_color_base
from photo_worker.v2.render_beauty import prepare_beauty_stage,render_beauty_params
from photo_worker.v2.render_portrait import prepare_portrait_base
from test_beauty_prepare import inputs
from test_beauty import params


@pytest.mark.parametrize("protected",[False,True])
def test_color_then_real_face_beauty_equivalence_and_evidence(tmp_path,protected):
    rgb,image,faces,_=inputs()
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    value.quality.status='needs_review'
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=params().model_dump(mode='python')
    data['models']=[dict(id='sfd',weight_id='fixture'),dict(id='fan2d',weight_id='fixture')]
    data['strength']=.4;effective=StyleV2.model_validate(data)
    zero=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
    style=PreparedRenderStyle(value,effective,effective,(),zero)
    state=SimpleNamespace(source=image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(protected_mask=None)))
    from photo_worker.v2.render_portrait import portrait_provider_peak
    from photo_worker.v2.render_resources import render_resources
    if protected:
        state.protection=SimpleNamespace(protected=True)
        state.recipe.recipe.protected_mask=ArtifactRef(artifact_id=uuid4(),sha256="a"*64)
    peak=portrait_provider_peak(image,style,tuple(faces),has_protection=protected)
    profile={'resource_estimate':render_resources(256,256,mask_refs=int(protected),mask_nodes=int(protected),provider_peak=peak)}
    masks=SimpleNamespace(protection=(lambda x,y,w,h:np.full((h,w),.001,np.float32)) if protected else None)
    original=AdjustmentPixels(rgb,None)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,30*1024**2,min_free_bytes=0) as scratch:
        artifacts=SimpleNamespace(scratch=scratch)
        with prepare_color_base(artifacts,state,replace(style,effective=value.model_copy(update={'strength':.4})),original,person_reliable=False,read_protected=masks.protection) as color:
            with prepare_beauty_stage(artifacts,image,color,render_beauty_params(style),tuple(faces)) as beauty:
                expected=beauty.pixels.rgb.copy();evidence=beauty.faces
        with prepare_portrait_base(artifacts,state,style,original,masks,faces=tuple(faces),person_reliable=False,execution_profile=profile) as result:
            np.testing.assert_array_equal(result.pixels.rgb.view(np.uint32),(rgb if protected else expected).view(np.uint32))
            assert result.beauty_evidence==evidence and result.review_required
        assert not list(scratch.directory.iterdir())


def test_actual_face_roi_and_final_protection_costs_are_admitted(tmp_path):
    from photo_worker.v2.render_portrait import portrait_provider_peak
    import pytest
    rgb,image,faces,_=inputs()
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=params().model_dump(mode='python')
    data['models']=[dict(id='sfd',weight_id='fixture'),dict(id='fan2d',weight_id='fixture')]
    value=StyleV2.model_validate(data)
    style=SimpleNamespace(effective=value)
    plain=portrait_provider_peak(image,style,tuple(faces),has_protection=False)
    protected=portrait_provider_peak(image,style,tuple(faces),has_protection=True)
    assert protected['scratch_bytes']-plain['scratch_bytes']==16*256*256+16384
    assert protected['ram_bytes']>protected['scratch_bytes'] and protected['vram_bytes']==0
    with pytest.raises(ValueError,match='RESOURCES_INVALID'):
        portrait_provider_peak(image,style,(faces[0],faces[0]),has_protection=True)


def test_missing_portrait_resource_budget_rejects_before_scratch_allocation(tmp_path):
    from photo_worker.runtime.executor import OperationFailure
    import pytest
    rgb,image,faces,_=inputs()
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=params().model_dump(mode='python')
    data['models']=[dict(id='sfd',weight_id='fixture'),dict(id='fan2d',weight_id='fixture')]
    style=SimpleNamespace(effective=StyleV2.model_validate(data))
    state=SimpleNamespace(source=image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(protected_mask=None)))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,30*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(OperationFailure,match='CONFIG_INVALID'):
            with prepare_portrait_base(SimpleNamespace(scratch=scratch),state,style,AdjustmentPixels(rgb,None),
                    SimpleNamespace(protection=None),faces=tuple(faces),person_reliable=False,
                    execution_profile={'resource_estimate':{}}):pass
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('cancel',[False,True])
@pytest.mark.parametrize('protected',[False,True])
def test_authored_denoise_then_real_color_owns_stages(tmp_path,cancel,protected):
    from test_image_denoise import Artifacts,Pool
    from photo_worker.contracts.restoration import DenoiseParams
    from photo_worker.v2.render_portrait import prepare_denoised_portrait_base
    from photo_worker.v2.render_denoise import prepare_authored_denoise
    from contextlib import contextmanager
    a=Artifacts(tmp_path);pool=Pool();params=DenoiseParams(image=a.image_ref,strength=.7,model_lock_id=pool.lock.lock_id,tile=256)
    ref=ArtifactRef(artifact_id=uuid4(),sha256='e'*64);a.manifests[ref.artifact_id]=params
    value=StyleV2.model_validate_json((Path(__file__).parents[2]/'docs/implementation/contracts/examples/style-fixed.example.json').read_bytes())
    value.quality.status='needs_review'
    data=value.model_dump(mode='python',by_alias=True)
    data['models'].append(dict(id=params.model_id,weight_id='fixture'))
    data['extensions']['restoration']=dict(denoise=params.model_id,denoise_strength=params.strength,upscale='off',sensor_dust='off')
    effective=StyleV2.model_validate(data)
    zero=CorrectionVector(ev=0,log_r=0,log_g=0,log_b=0,saturation_delta=0,retouch_delta=0)
    style=PreparedRenderStyle(value,effective,effective,(),zero)
    plain=replace(style,effective=value)
    state=SimpleNamespace(source=a.image,layers=None,protection=None,recipe=SimpleNamespace(recipe=SimpleNamespace(
        source=SimpleNamespace(source=a.image_ref),protected_mask=None)))
    masks=SimpleNamespace(protection=(lambda x,y,w,h:np.full((h,w),.001,np.float32)) if protected else None)
    if protected:
        state.protection=SimpleNamespace(protected=True)
        state.recipe.recipe.protected_mask=ArtifactRef(artifact_id=uuid4(),sha256='f'*64)
    peak=dict(ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0)
    profile=dict(backend='cuda',resource_estimate=dict(pixel_count=480,ram_bytes=8*1024**3,
        scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3))
    original=AdjustmentPixels(a.source,None);cancelled=False
    def active():
        if cancelled:raise InterruptedError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,10*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        with prepare_authored_denoise(a,a.image_ref,effective.extensions.restoration,ref,original,pool,
                backend='cuda',execution_profile=profile,provider_peak=peak,verify_runtime=lambda:None) as denoised:
            with prepare_portrait_base(a,state,plain,denoised.pixels,masks,faces=(),person_reliable=False,
                    execution_profile=profile) as portrait:expected=portrait.pixels.rgb.copy()
        if protected:expected=a.source.copy()
        from photo_worker.v2.render_native_base import NativeDenoiseProvider,NativeBaseEvidence,NativePortraitBackgroundFactory
        @contextmanager
        def evidence(*args):yield NativeBaseEvidence((),False,denoise_params_ref=ref)
        provider=NativeDenoiseProvider(pool,backend='cuda',provider_peak=peak,verify_runtime=lambda:None)
        factory=NativePortraitBackgroundFactory(object(),evidence_factory=lambda *args:evidence,
            retained_resources=lambda *args:dict(ram_bytes=0,scratch_bytes=0),denoise_provider=provider)
        if not cancel:
            from photo_worker.v2.render_portrait import denoised_portrait_provider_peak
            from photo_worker.v2.render_resources import render_resources
            from photo_worker.runtime.executor import OperationFailure
            full=denoised_portrait_provider_peak(a.image,style,params,(),provider_peak=peak,
                has_protection=protected,tile_size=256)
            estimate=render_resources(24,20,mask_refs=int(protected),mask_nodes=int(protected),
                provider_peak=full,tile_size=256)
            calls_before=len(pool.engine.calls)
            for resource,code in [('ram_bytes','RAM_ADMISSION_EXCEEDED'),('scratch_bytes','SCRATCH_ADMISSION_EXCEEDED')]:
                bad=dict(estimate);bad[resource]-=1
                rejected=factory(a,state,dict(backend='cuda',tile_size=256,resource_estimate=bad),active)
                with pytest.raises(OperationFailure,match=code):
                    with rejected(state,style,original,masks):pytest.fail('Underfunded chain executed')
                assert len(pool.engine.calls)==calls_before
                assert not list(scratch.directory.iterdir())
            assert peak==dict(ram_bytes=0,scratch_bytes=0,vram_bytes=0,output_bytes=0)
        callback=factory(a,state,dict(profile,tile_size=256),active)
        def consume():
            nonlocal cancelled
            with callback(state,style,original,masks) as result:
                np.testing.assert_array_equal(result.pixels.rgb.view(np.uint32),expected.view(np.uint32))
                assert len(result.effects)==2 and result.effects[0].report.strength==params.strength
                from photo_worker.v2.render_effects import effect_review_required,unpack_render_base
                pixels,evidence=unpack_render_base(result)
                assert pixels is result.pixels and evidence is result
                assert effect_review_required(evidence) is True
                cancelled=cancel
        if cancel:
            with pytest.raises(InterruptedError):consume()
        else:consume()
        assert not list(scratch.directory.iterdir())

from photo_worker.contracts.base import canonical_bytes
import numpy as np
import pytest
from uuid import uuid4
from photo_worker.algorithms.portrait_detail import apply_region
from photo_worker.contracts.images import MaskRef
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.contracts.style import PortraitDetail
from photo_worker.operations.portrait.detail import execute
from photo_worker.runtime.executor import OperationFailure
from photo_worker.runtime.scratch import ScratchSpace
from test_asset_adapters import context_for
from test_image_color_adapter import Artifacts


def fixture():
    y,x=np.mgrid[:64,:64];rgb=np.stack([.35+x/256,.32+y/320,.3+x/512],axis=-1).astype(np.float32);mask=np.zeros((64,64),np.float32);mask[16:48,16:48]=1;protected=np.zeros_like(mask);return rgb,mask,protected


def test_teeth_bounds_and_outside_exact():
    rgb,mask,protected=fixture();out,count,ev,chroma,reason=apply_region(rgb,mask,'teeth',1,protected)
    assert reason is None and count==32*32 and ev==.2 and chroma==.25
    assert np.array_equal(out[mask==0],rgb[mask==0]) and np.mean(out[mask==1])>np.mean(rgb[mask==1])


def test_protected_pixels_remain_exact_and_glasses_is_review():
    rgb,mask,protected=fixture();protected[24:40,24:40]=1;out,*_=apply_region(rgb,mask,'shine',1,protected)
    assert np.array_equal(out[protected==1],rgb[protected==1])
    out,count,ev,chroma,reason=apply_region(rgb,mask,'glasses',1,protected)
    assert reason=='review_required' and count==0 and np.array_equal(out,rgb)


def adapter_fixture(root):
    a=Artifacts(root,False);body=a.resolved.model_dump(exclude={'hash'});body['strength']=1.;extensions=a.resolved.modules.extensions.model_copy(update={'portrait_detail':PortraitDetail(enabled=True,teeth_strength=.8,shine_strength=.5,under_eye_strength=.4,glasses_policy='review')});body['modules']['extensions']=extensions;a.resolved=ResolvedStyle.create(**body);resolved=a.add(a.resolved)
    values=np.zeros(a.rgb.shape[:2],np.float32);values[8:30,200:400]=1;mask_ref=a.array(root,'teeth-mask',values);mask=MaskRef(artifact=mask_ref,canvas=a.image.canvas,transform_chain=a.image.transform_chain,selector='manual',confidence=1.,model_lock_id=None,protected=False)
    a.raw={'image':a.image_ref.model_dump(mode='json'),'resolved':resolved.model_dump(mode='json'),'regions':[{'region_id':'teeth-1','kind':'teeth','mask':mask.model_dump(mode='json'),'confirmed':False,'strength':1}],'protected':[]}
    ctx=context_for(a.image_ref,'portrait.detail');ctx.lease.command.inputs=[a.image_ref,a.image.pixels,a.image.alpha,resolved,mask_ref];ctx.lease.execution_profile.update(resource_estimate={'ram_bytes':1073741824},tile_size=512)
    return a,ctx


def test_portrait_detail_adapter_uses_declared_explicit_region(tmp_path):
    a,ctx=adapter_fixture(tmp_path);root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,2_000_000,min_free_bytes=0) as scratch:a.scratch=scratch;result=execute(ctx,a)
    assert len(result.outputs)==3 and a.staged[1][1].regions[0].applied
    assert not np.array_equal(a.output,a.rgb)


def test_portrait_detail_adapter_rejects_undeclared_region(tmp_path):
    a,ctx=adapter_fixture(tmp_path);ctx.lease.command.inputs.pop()
    with pytest.raises(OperationFailure) as error:execute(ctx,a)
    assert error.value.error.code=='INPUT_INVALID' and not a.uploads


def test_scoped_detail_matches_existing_ordered_algorithm_and_owns_metadata(tmp_path):
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.algorithms.effective import resolve_effective_params
    from photo_worker.operations.portrait.prepare_detail import prepare_detail
    a,ctx=adapter_fixture(tmp_path);params=PortraitParams.model_validate_json(canonical_bytes(a.raw))
    first=params.regions[0].model_copy(update={'region_id':'b','kind':'shine','strength':.6})
    second=params.regions[0].model_copy(update={'region_id':'a','kind':'teeth','strength':.8})
    regions=[first,second];detail=resolve_effective_params(a.resolved).modules.extensions.portrait_detail
    protection=np.zeros(a.rgb.shape[:2],np.float32);protection[10:15,220:240]=1
    mask=np.zeros_like(protection);mask[8:30,200:400]=1
    expected=a.rgb.copy()
    for entry in sorted(regions,key=lambda entry:entry.region_id):
        strength={'teeth':detail.teeth_strength,'shine':detail.shine_strength}[entry.kind]*entry.strength
        expected,*_=apply_region(expected,mask,entry.kind,strength,protection)
    original=a.rgb.copy()
    def read(reference):
        first.strength=0;second.strength=0
        return mask
    with prepare_detail(a.rgb,regions,detail,read,protection) as result:
        np.testing.assert_array_equal(result.rgb.view(np.uint32),expected.view(np.uint32))
        assert [region.region_id for region in result.regions]==['a','b']
        assert not result.rgb.flags.writeable
        np.testing.assert_array_equal(result.rgb[protection==1].view(np.uint32),original[protection==1].view(np.uint32))
    np.testing.assert_array_equal(a.rgb.view(np.uint32),original.view(np.uint32))


def test_scoped_detail_rejects_invalid_masks_and_cancels_after_yield(tmp_path):
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.algorithms.effective import resolve_effective_params
    from photo_worker.operations.portrait.prepare_detail import prepare_detail
    a,ctx=adapter_fixture(tmp_path);params=PortraitParams.model_validate_json(canonical_bytes(a.raw))
    detail=resolve_effective_params(a.resolved).modules.extensions.portrait_detail
    protection=np.zeros(a.rgb.shape[:2],np.float32);mask=np.ones_like(protection)
    invalid=mask.copy();invalid[0,0]=np.nan
    with pytest.raises(ValueError,match='INPUT_INVALID'):
        with prepare_detail(a.rgb,params.regions,detail,lambda ref:invalid,protection):pass
    cancelled=False
    def check():
        if cancelled:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError,match='cancelled'):
        with prepare_detail(a.rgb,params.regions,detail,lambda ref:mask,protection,check_active=check):
            cancelled=True


def test_detail_peak_counts_full_frame_native_memory():
    from photo_worker.operations.portrait.prepare_detail import detail_peak
    assert detail_peak(64,64)['ram_bytes']==1073741824
    assert detail_peak(10000,10000)['ram_bytes']==16001048576
    assert detail_peak(10000,10000)['scratch_bytes']==1200008192
    for dimensions in ((True,1),(0,1),(200000001,1)):
        with pytest.raises(ValueError):detail_peak(*dimensions)


@pytest.mark.parametrize('variant',['applied','changed','glasses'])
def test_render_pinned_detail_real_masks_source_binding_and_review(tmp_path,variant):
    from photo_worker.contracts.portrait_detail import PortraitParams
    from photo_worker.v2.render_detail import prepare_pinned_detail
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    from photo_worker.v2.render_effects import unpack_render_base,effect_review_required
    a,ctx=adapter_fixture(tmp_path)
    alpha=np.full(a.rgb.shape[:2],.75,np.float32)
    a.image=a.image.model_copy(update={'alpha':a.array(tmp_path,'render-alpha',alpha)})
    a.image_ref=a.add(a.image)
    values=a.resolved.model_dump(mode='python',exclude={'hash'})
    values['params']=a.resolved.params.model_copy(update={'image':a.image_ref})
    params=PortraitParams.model_validate_json(canonical_bytes(a.raw))
    params.image=a.image_ref
    params.resolved=a.add(ResolvedStyle.create(**values))
    if variant=='glasses':
        params.regions[0].confirmed=True;params.regions[0].kind='glasses'
    reference=a.add(params);rgb=a.rgb.copy()
    if variant=='changed':rgb[0,0,0]=.2
    root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        # This fixture already declares float32 alpha; load its exact bytes.
        with a.input(a.image.alpha,suffix='.npy') as local:alpha=np.load(local.path).copy()
        def render():
            return prepare_pinned_detail(a,reference,AdjustmentPixels(rgb,alpha),
                execution_profile={'resource_estimate':dict(pixel_count=rgb.shape[0]*rgb.shape[1],
                    ram_bytes=8*1024**3,scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)})
        if variant=='changed':
            with pytest.raises(ValueError,match='CURRENT_PIXELS_CHANGED'):
                with render():pass
        else:
            with render() as stage:
                pixels,evidence=unpack_render_base(stage)
                assert evidence is stage and pixels.alpha is alpha
                assert stage.regions[0].applied==(variant=='applied')
                if variant=='glasses':
                    assert effect_review_required(stage)
                    np.testing.assert_array_equal(pixels.rgb,rgb)
                else:assert not np.array_equal(pixels.rgb,rgb)
        if variant!='changed':
            from types import SimpleNamespace
            from photo_worker.v2.render_detail import PinnedDetailProviderFactory
            from photo_worker.v2.render_layer_params import LoadedRenderLayer,RenderLayerOperator
            from photo_worker.v2.render_layers import RenderLayer
            from photo_worker.v2.render_providers import RenderLayerProvider,RenderLayerProviders
            from photo_worker.v2.render_adjustments import compose_render_adjustments
            from photo_worker.v2.render_effects import RenderCombinedEffects
            layer=RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='portrait.detail',
                operation_version='1',params=reference,mask={'type':'all'})
            item=LoadedRenderLayer(layer,params,'b'*64)
            profile={'resource_estimate':dict(pixel_count=rgb.shape[0]*rgb.shape[1],ram_bytes=8*1024**3,
                scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
            factory=PinnedDetailProviderFactory(runtime_sha256='b'*64,verify_runtime=lambda:None,
                validate_binding=lambda params,inputs:None)
            dispatch=RenderLayerProviders(factory(a,SimpleNamespace(),profile,lambda:None))
            masks=SimpleNamespace(layer=lambda id,x,y,w,h:np.ones((h,w),np.float32),protection=lambda x,y,w,h:None)
            with compose_render_adjustments(rgb,alpha,(item,),masks,scratch,dispatch,color=a.image.color,
                    max_tile_bytes=4*1024**2,tile_size=256) as result:
                assert result.effects[0].regions[0].applied==(variant=='applied')
                assert effect_review_required(RenderCombinedEffects(None,result.effects))==(variant=='glasses')
                np.testing.assert_array_equal(result.alpha,alpha)
                if variant=='applied':assert not np.array_equal(result.rgb,rgb)
                else:np.testing.assert_array_equal(result.rgb,rgb)
        assert not list(scratch.directory.iterdir())

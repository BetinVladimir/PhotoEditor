from __future__ import annotations

from contextlib import contextmanager
from datetime import datetime, timedelta, timezone
import hashlib
import json
from types import SimpleNamespace
from uuid import uuid4

import numpy as np
import pytest

from photo_worker.algorithms.model_delta import transfer_model_delta, work_to_model
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.images import Canvas, ColorSpace, TransformChain, WorkingImage
from photo_worker.contracts.restoration import DenoiseParams, DenoiseReport
from photo_worker.operations.image.denoise import execute
from photo_worker.operations.image.upscale import execute as execute_upscale
from photo_worker.runtime.database import Lease
from photo_worker.runtime.executor import ExecutionContext, OperationFailure
from photo_worker.runtime.scratch import ScratchSpace


LOCK = "a" * 64


def test_model_delta_identity_strength_and_unsafe_gamut():
    source = np.full((12, 13, 3), .45, np.float32)
    model, clipped = work_to_model(source)
    identity, evidence = transfer_model_delta(source, model, model, .8)
    assert not clipped.any() and not evidence.disabled.any()
    assert np.array_equal(identity, source)
    changed, evidence = transfer_model_delta(source, model, np.clip(model + .03, 0, 1), 1)
    assert np.isfinite(changed).all() and changed.mean() > source.mean() and not evidence.disabled.any()
    zero, _ = transfer_model_delta(source, model, np.clip(model + .03, 0, 1), 0)
    assert np.array_equal(zero, source)
    saturated = source.copy(); saturated[..., 0] = 1
    model, _ = work_to_model(saturated)
    protected, evidence = transfer_model_delta(saturated, model, np.zeros_like(model), 1)
    assert evidence.source_clipped.all() and np.array_equal(protected, saturated)


class Engine:
    def __init__(self, delta=.02): self.delta = delta; self.calls = []
    def infer(self, rgb):
        self.calls.append(rgb.shape)
        return np.clip(rgb + self.delta, 0, 1)


class Pool:
    def __init__(self, lock=LOCK, model="scunet_color_real_psnr"): self.lock = SimpleNamespace(lock_id=lock); self.engine = Engine(); self.model=model
    @contextmanager
    def acquire(self, model_id, backend):
        assert model_id == self.model and backend == "cuda"
        yield self.engine


class Artifacts:
    def __init__(self, root, *, strength=.7, lock=LOCK, alpha=False):
        self.source = np.full((20, 24, 3), .4, np.float32)
        source_path = root / "source.npy"; np.save(source_path, self.source, allow_pickle=False)
        self.pixel_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(source_path.read_bytes()).hexdigest())
        self.paths = {self.pixel_ref.artifact_id: source_path}; self.uploads = []; self.staged = []
        canvas = Canvas(width=24, height=20, orientation=1, space_id="source", source_sha256="b" * 64)
        color = ColorSpace(profile_id="work_rec2020_gamma24_v1", icc_sha256="c" * 64, encoding="gamma24")
        alpha_ref = ArtifactRef(artifact_id=uuid4(), sha256="d" * 64) if alpha else None
        self.image = WorkingImage(pixels=self.pixel_ref, canvas=canvas, color=color, dtype="float32",
                                  alpha=alpha_ref, transform_chain=TransformChain.create([]))
        self.image_ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(self.image)).hexdigest())
        self.manifests = {self.image_ref.artifact_id: self.image}
        self.raw = DenoiseParams(image=self.image_ref, strength=strength, model_lock_id=lock, tile=256).model_dump(mode="json")

    def params(self, model): return model.model_validate_json(json.dumps(self.raw))
    def contract(self, ref, model): return self.manifests[ref.artifact_id]
    @contextmanager
    def input(self, ref, *, suffix): yield SimpleNamespace(path=self.paths[ref.artifact_id])
    def stage(self, path, reservation, role, media_type):
        reservation.validate_file(path); self.output = np.load(path, allow_pickle=False)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        self.uploads.append(ref); return ref
    def stage_contract(self, value, role):
        self.staged.append((role, value)); return ArtifactRef(artifact_id=uuid4(), sha256=hashlib.sha256(canonical_bytes(value)).hexdigest())


def context(artifacts):
    now = datetime.now(timezone.utc)
    command = SimpleNamespace(inputs=[artifacts.image_ref, artifacts.pixel_ref] + ([artifacts.image.alpha] if artifacts.image.alpha else []))
    lease = Lease(command=command, worker_id=uuid4(), fence=1, until=now + timedelta(minutes=5),
                  deadline=now + timedelta(minutes=5), execution_profile={"backend":"cuda", "resource_estimate":{"ram_bytes":1073741824}}, input_digest="e"*64)
    return ExecutionContext(lease)


def test_denoise_adapter_tiles_preserves_metadata_and_reports(tmp_path):
    artifacts = Artifacts(tmp_path); pool = Pool(); ctx = context(artifacts)
    root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 1_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch; result = execute(ctx, artifacts, pool, "cuda")
    assert pool.engine.calls == [(20, 24, 3)]
    assert artifacts.output.shape == artifacts.source.shape and artifacts.output.mean() > artifacts.source.mean()
    image = artifacts.staged[0][1]; report = artifacts.staged[1][1]
    assert image.canvas == artifacts.image.canvas and image.color == artifacts.image.color and image.alpha == artifacts.image.alpha
    assert report.backend == "cuda" and report.model_lock_id == LOCK and report.alpha_preserved is False
    DenoiseReport.model_validate_json(canonical_bytes(report)); assert len(result.outputs) == 3


def test_denoise_rejects_lock_before_materializing(tmp_path):
    artifacts = Artifacts(tmp_path, lock="f" * 64); pool = Pool(); ctx = context(artifacts)
    with pytest.raises(OperationFailure) as error: execute(ctx, artifacts, pool, "cuda")
    assert error.value.error.code == "MODEL_LOCK_MISMATCH" and not artifacts.uploads


def test_upscale_adapter_builds_new_canvas_and_transform(tmp_path):
    artifacts = Artifacts(tmp_path)
    artifacts.raw = {"image": artifacts.image_ref.model_dump(mode="json"), "scale": 2,
                     "model_id": "realesrgan_x2", "model_lock_id": LOCK,
                     "color_adapter_id": "work_srgb_delta_v1", "tile": 256, "overlap": 32}
    pool = Pool(model="realesrgan_x2")
    def infer(rgb):
        import cv2
        return cv2.resize(rgb, (rgb.shape[1] * 2, rgb.shape[0] * 2), interpolation=cv2.INTER_CUBIC).clip(0, 1)
    pool.engine.infer = infer
    ctx = context(artifacts); root = tmp_path / "scratch"; root.mkdir()
    with ScratchSpace(root, uuid4(), uuid4(), 1, 2_000_000, min_free_bytes=0) as scratch:
        artifacts.scratch = scratch; result = execute_upscale(ctx, artifacts, pool, "cuda")
    image = artifacts.staged[0][1]; report = artifacts.staged[1][1]
    assert artifacts.output.shape == (40, 48, 3) and image.canvas.width == 48 and image.canvas.height == 40
    assert len(image.transform_chain.steps) == 1 and image.transform_chain.steps[0].kind == "resize"
    assert report.scale == 2 and report.output_pixel_count == 1920 and len(result.outputs) == 3


def test_scoped_denoise_multitile_matches_original_delta_and_owns_params(tmp_path):
    from photo_worker.operations.image.prepare_denoise import prepare_denoise
    source=np.random.default_rng(4).uniform(.3,.5,(273,519,3)).astype(np.float32)
    params=DenoiseParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),strength=.7,
        model_lock_id=LOCK,tile=256)
    class MutatingEngine(Engine):
        def infer(self,rgb):
            params.strength=0
            return super().infer(rgb)
    engine=MutatingEngine();expected=np.empty_like(source);counts=[0,0,0]
    reference=Engine()
    for y in range(0,273,256):
        for x in range(0,519,256):
            y1=min(y+256,273);x1=min(x+256,519)
            hy=max(0,y-64);hx=max(0,x-64);ey=min(273,y1+64);ex=min(519,x1+64)
            work=source[hy:ey,hx:ex];model,_=work_to_model(work)
            restored,evidence=transfer_model_delta(work,model,reference.infer(model),.7)
            core=(slice(y-hy,y1-hy),slice(x-hx,x1-hx))
            expected[y:y1,x:x1]=restored[core]
            for index,name in enumerate(('source_clipped','gamut_excess','disabled')):
                counts[index]+=np.count_nonzero(getattr(evidence,name)[core])
    original=source.copy();progress=[]
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_denoise(source,params,engine,scratch,progress=lambda done,total:progress.append((done,total))) as prepared:
            np.testing.assert_array_equal(np.load(prepared.path).view(np.uint32),expected.view(np.uint32))
            assert [prepared.source_clip,prepared.gamut_excess,prepared.disabled]==counts
            path=prepared.path
        assert not path.exists() and not list(scratch.directory.iterdir())
    assert len(engine.calls)==6 and progress[-1]==(6,8)
    np.testing.assert_array_equal(source.view(np.uint32),original.view(np.uint32))


def test_scoped_denoise_cancel_after_yield_releases_output(tmp_path):
    from photo_worker.operations.image.prepare_denoise import prepare_denoise
    source=np.full((12,13,3),.4,np.float32)
    params=DenoiseParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),strength=.7,model_lock_id=LOCK)
    cancelled=False
    def check():
        if cancelled:raise RuntimeError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_denoise(source,params,Engine(),scratch,check_active=check) as prepared:
                assert prepared.path.exists();cancelled=True
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('variant',['pass','changed','lock','budget','gamut'])
def test_render_denoise_pinned_source_model_admission_and_evidence(tmp_path,variant):
    from photo_worker.v2.render_denoise import prepare_authored_denoise
    from photo_worker.contracts.style import Restoration
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    from photo_worker.v2.render_effects import unpack_render_base,effect_review_required
    a=Artifacts(tmp_path);pool=Pool();params=DenoiseParams.model_validate_json(canonical_bytes(a.raw))
    ref=ArtifactRef(artifact_id=uuid4(),sha256='f'*64);a.manifests[ref.artifact_id]=params
    if variant=='gamut':
        a.source[...,0]=1;path=a.paths[a.pixel_ref.artifact_id];np.save(path,a.source)
        a.pixel_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(path.read_bytes()).hexdigest())
        a.paths[a.pixel_ref.artifact_id]=path
        a.image=a.image.model_copy(update={'pixels':a.pixel_ref})
        a.image_ref=ArtifactRef(artifact_id=uuid4(),sha256=hashlib.sha256(canonical_bytes(a.image)).hexdigest())
        a.manifests[a.image_ref.artifact_id]=a.image
        params.image=a.image_ref
    current=a.source.copy()
    if variant=='changed':current[0,0,0]=.1
    if variant=='lock':pool.lock.lock_id='c'*64
    estimate=dict(pixel_count=current.shape[0]*current.shape[1],ram_bytes=8*1024**3,
        scratch_bytes=8*1024**3,vram_bytes=8*1024**3,output_bytes=8*1024**3)
    if variant=='budget':estimate['ram_bytes']=0
    verified=[];root=tmp_path/'scratch';root.mkdir()
    with ScratchSpace(root,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        a.scratch=scratch
        def prepare():
            return prepare_authored_denoise(a,params.image,Restoration(denoise=params.model_id,
                denoise_strength=params.strength,upscale='off',sensor_dust='off'),
                ref,AdjustmentPixels(current,None),pool,backend='cuda',
                execution_profile={'backend':'cuda','resource_estimate':estimate},
                provider_peak=dict(ram_bytes=1073741824,scratch_bytes=0,vram_bytes=0,output_bytes=0),
                verify_runtime=lambda:verified.append('runtime'))
        if variant in ('changed','lock','budget'):
            with pytest.raises((ValueError,OperationFailure)):
                with prepare():pytest.fail('invalid binding rendered')
            assert pool.engine.calls==[]
        else:
            with prepare() as stage:
                pixels,evidence=unpack_render_base(stage)
                assert evidence is stage and stage.report.image==a.image_ref
                assert not pixels.rgb.flags.writeable
                assert effect_review_required(stage)==(variant=='gamut')
                if variant=='gamut':np.testing.assert_array_equal(pixels.rgb,current)
                else:assert pixels.rgb.mean()>current.mean()
        assert verified==['runtime'] and not list(scratch.directory.iterdir())
        if variant in ('pass','gamut'):
            from photo_worker.v2.render_denoise import PinnedDenoiseProviderFactory
            from photo_worker.v2.render_layer_params import LoadedRenderLayer,RenderLayerOperator
            from photo_worker.v2.render_layers import RenderLayer
            from photo_worker.v2.render_providers import RenderLayerProvider,RenderLayerProviders
            from photo_worker.v2.render_adjustments import compose_render_adjustments
            from photo_worker.v2.render_effects import RenderCombinedEffects
            layer=RenderLayer(id=uuid4(),enabled=True,opacity=1,blend='normal',operation='image.denoise',
                operation_version='1',params=ref,mask={'type':'all'})
            factory=PinnedDenoiseProviderFactory(pool,backend='cuda',
                provider_peak=dict(ram_bytes=1073741824,scratch_bytes=0,vram_bytes=0,output_bytes=0),
                runtime_sha256='b'*64,verify_runtime=lambda:verified.append('native'),
                validate_binding=lambda p,i:None)
            providers=factory(a,SimpleNamespace(),{'backend':'cuda','resource_estimate':estimate},lambda:None)
            dispatch=RenderLayerProviders(providers)
            masks=SimpleNamespace(layer=lambda id,x,y,w,h:np.ones((h,w),np.float32),protection=lambda x,y,w,h:None)
            with compose_render_adjustments(current,None,(LoadedRenderLayer(layer,params,'b'*64),),masks,
                    scratch,dispatch,color=a.image.color,max_tile_bytes=4*1024**2,tile_size=256) as result:
                assert result.effects[0].denoise_report.image==a.image_ref
                assert effect_review_required(RenderCombinedEffects(None,result.effects))==(variant=='gamut')
                if variant=='gamut':np.testing.assert_array_equal(result.rgb,current)
                else:assert result.rgb.mean()>current.mean()
            assert verified==['runtime','native','native'] and not list(scratch.directory.iterdir())


@pytest.mark.parametrize('scale',[2,4])
@pytest.mark.parametrize('alpha_dtype',['uint16','float32'])
def test_scoped_upscale_matches_native_halo_pixels_and_alpha(tmp_path,scale,alpha_dtype):
    import cv2
    from photo_worker.contracts.restoration import UpscaleParams
    from photo_worker.operations.image.prepare_upscale import prepare_upscale
    from photo_worker.algorithms.model_delta import resize_work_linear
    source=np.random.default_rng(5).uniform(.3,.5,(261,259,3)).astype(np.float32)
    alpha=np.random.default_rng(6).random(source.shape[:2],dtype=np.float32)
    if alpha_dtype=='uint16':alpha=(alpha*65535).astype(np.uint16)
    params=UpscaleParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),scale=scale,
        model_id=f'realesrgan_x{scale}',model_lock_id=LOCK)
    class ResizeEngine:
        def infer(self,value):
            return np.clip(cv2.resize(value,(value.shape[1]*scale,value.shape[0]*scale),interpolation=cv2.INTER_CUBIC),0,1).astype(np.float32)
    engine=ResizeEngine();expected=np.empty((261*scale,259*scale,3),np.float32);counts=[0,0,0]
    for y in range(0,261,256):
        for x in range(0,259,256):
            ey=min(y+256,261);ex=min(x+256,259);hy=max(0,y-32);hx=max(0,x-32)
            work=source[hy:min(261,ey+32),hx:min(259,ex+32)]
            model,_=work_to_model(work);model_out=engine.infer(model)
            base=resize_work_linear(work,(work.shape[1]*scale,work.shape[0]*scale))
            restored,evidence=transfer_model_delta(base,model_out,model_out,1.)
            core=(slice((y-hy)*scale,(ey-hy)*scale),slice((x-hx)*scale,(ex-hx)*scale))
            expected[y*scale:ey*scale,x*scale:ex*scale]=restored[core]
            for index,name in enumerate(('source_clipped','gamut_excess','disabled')):
                counts[index]+=np.count_nonzero(getattr(evidence,name)[core])
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,32*1024**2,min_free_bytes=0) as scratch:
        with prepare_upscale(source,alpha,params,engine,scratch) as result:
            np.testing.assert_array_equal(np.load(result.rgb_path).view(np.uint32),expected.view(np.uint32))
            np.testing.assert_array_equal(np.load(result.alpha_path),cv2.resize(alpha,(259*scale,261*scale),interpolation=cv2.INTER_LINEAR).astype(alpha.dtype))
            assert [result.source_clip,result.gamut_excess,result.disabled]==counts
        assert not list(scratch.directory.iterdir())


def test_scoped_upscale_invalid_model_closes_both_mappings(tmp_path):
    from photo_worker.contracts.restoration import UpscaleParams
    from photo_worker.operations.image.prepare_upscale import prepare_upscale
    params=UpscaleParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),scale=2,
        model_id='realesrgan_x2',model_lock_id=LOCK)
    source=np.full((12,13,3),.4,np.float32);alpha=np.ones((12,13),np.float32)
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        with pytest.raises(ValueError,match='MODEL_OUTPUT_INVALID'):
            with prepare_upscale(source,alpha,params,Engine(),scratch):pass
        assert not list(scratch.directory.iterdir())


def test_scoped_upscale_cancel_after_yield_releases_rgb_and_alpha(tmp_path):
    import cv2
    from photo_worker.contracts.restoration import UpscaleParams
    from photo_worker.operations.image.prepare_upscale import prepare_upscale
    params=UpscaleParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),scale=2,
        model_id='realesrgan_x2',model_lock_id=LOCK)
    engine=SimpleNamespace(infer=lambda value:cv2.resize(value,(value.shape[1]*2,value.shape[0]*2)))
    cancelled=False
    def check():
        if cancelled:raise RuntimeError('cancelled')
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        with pytest.raises(RuntimeError,match='cancelled'):
            with prepare_upscale(np.full((12,13,3),.4,np.float32),np.ones((12,13),np.float32),params,
                    engine,scratch,check_active=check,retain_baseline=True) as result:
                assert result.rgb_path.exists() and result.alpha_path.exists() and result.baseline_path.exists();cancelled=True
        assert not list(scratch.directory.iterdir())


@pytest.mark.parametrize('scale',[2,4])
def test_upscale_geometry_preserves_source_and_prior_transform_chain(tmp_path,scale):
    from photo_worker.operations.image.prepare_upscale import upscale_geometry
    from photo_worker.contracts.restoration import UpscaleParams
    a=Artifacts(tmp_path)
    first=UpscaleParams(image=a.image_ref,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    canvas,chain=upscale_geometry(a.image,first)
    image=a.image.model_copy(update={'canvas':canvas,'transform_chain':chain})
    params=UpscaleParams(image=a.image_ref,scale=scale,model_id=f'realesrgan_x{scale}',model_lock_id=LOCK)
    final,transforms=upscale_geometry(image,params)
    assert final.width==24*2*scale and final.height==20*2*scale
    assert final.source_sha256==a.image.canvas.source_sha256
    assert transforms.steps[0]==chain.steps[0]
    assert transforms.steps[-1].from_canvas==canvas and transforms.steps[-1].to_canvas==final
    assert transforms.steps[-1].pixel_centers=='half_pixel'
    assert transforms.steps[-1].valid_region.width==final.width
    assert image.canvas==canvas and len(image.transform_chain.steps)==1


def test_upscale_protection_restores_exact_linear_baseline(tmp_path):
    import cv2
    from photo_worker.contracts.restoration import UpscaleParams
    from photo_worker.operations.image.prepare_upscale import prepare_upscale
    from photo_worker.algorithms.model_delta import resize_work_linear
    params=UpscaleParams(image=ArtifactRef(artifact_id=uuid4(),sha256='a'*64),scale=2,
        model_id='realesrgan_x2',model_lock_id=LOCK)
    source=np.random.default_rng(7).uniform(.3,.5,(12,13,3)).astype(np.float32)
    alpha=np.full((12,13),.75,np.float32)
    protection=np.zeros((24,26),np.float32);protection[4:20,5:21]=.001
    class ChangedEngine:
        def infer(self,value):return np.clip(cv2.resize(value,(value.shape[1]*2,value.shape[0]*2),interpolation=cv2.INTER_CUBIC)+.03,0,1)
    baseline=resize_work_linear(source,(26,24))
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,1024*1024,min_free_bytes=0) as scratch:
        with prepare_upscale(source,alpha,params,ChangedEngine(),scratch,
                read_protected=lambda x,y,w,h:protection[y:y+h,x:x+w]) as result:
            pixels=np.load(result.rgb_path)
            np.testing.assert_array_equal(pixels[protection>0].view(np.uint32),baseline[protection>0].view(np.uint32))
            assert pixels[protection==0].mean()>baseline[protection==0].mean()
            np.testing.assert_array_equal(np.load(result.alpha_path),cv2.resize(alpha,(26,24),interpolation=cv2.INTER_LINEAR))
        with pytest.raises(ValueError,match='PROTECTION_INVALID'):
            with prepare_upscale(source,None,params,ChangedEngine(),scratch,
                    read_protected=lambda x,y,w,h:np.full((h,w),np.nan,np.float32)):pass
        assert not list(scratch.directory.iterdir())


def test_retained_upscale_baseline_restores_transformed_protection_without_reinference(tmp_path):
    import cv2
    from photo_worker.contracts.restoration import UpscaleParams
    from photo_worker.operations.image.prepare_upscale import prepare_upscale,upscale_geometry
    from photo_worker.v2.render_geometry_masks import prepare_resize_masks
    from photo_worker.v2.render_protection import preserve_render_protection
    from photo_worker.v2.render_adjustments import AdjustmentPixels
    from photo_worker.contracts.images import MaskRef
    a=Artifacts(tmp_path);params=UpscaleParams(image=a.image_ref,scale=2,model_id='realesrgan_x2',model_lock_id=LOCK)
    canvas,chain=upscale_geometry(a.image,params)
    ref=MaskRef(artifact=a.pixel_ref,canvas=a.image.canvas,transform_chain=a.image.transform_chain,
        selector='manual',confidence=1.,model_lock_id=None,protected=True)
    values=np.zeros(a.source.shape[:2],np.float32);values[4:16,5:19]=1
    class ChangedEngine:
        calls=0
        def infer(self,value):
            self.calls+=1
            return np.clip(cv2.resize(value,(value.shape[1]*2,value.shape[0]*2),interpolation=cv2.INTER_CUBIC)+.03,0,1)
    engine=ChangedEngine();profile={'resource_estimate':dict(pixel_count=40*48,ram_bytes=8*1024**3,
        scratch_bytes=8*1024**3,vram_bytes=0,output_bytes=8*1024**3)}
    with ScratchSpace(tmp_path,uuid4(),uuid4(),1,4*1024**2,min_free_bytes=0) as scratch:
        with prepare_upscale(a.source,None,params,engine,scratch,retain_baseline=True) as native:
            baseline=np.load(native.baseline_path).copy();candidate=np.load(native.rgb_path).copy()
            with prepare_resize_masks(((ref,values),),values,chain.steps[-1],a.image.transform_chain,
                    candidate,scratch,execution_profile=profile,tile=256) as transformed:
                protected=transformed.masks[0][1]
                final=AdjustmentPixels(candidate,None,transformed.generated)
                with preserve_render_protection(SimpleNamespace(scratch=scratch),AdjustmentPixels(baseline,None),final,
                        lambda x,y,w,h:protected[y:y+h,x:x+w],tile_size=256) as restored:
                    selected=protected>0
                    np.testing.assert_array_equal(restored.rgb[selected].view(np.uint32),baseline[selected].view(np.uint32))
                    np.testing.assert_array_equal(restored.rgb[~selected],candidate[~selected])
                    assert restored.generated is transformed.generated
        assert engine.calls==1 and not list(scratch.directory.iterdir())

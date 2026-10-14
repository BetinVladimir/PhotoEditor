"""Admitted pinned denoise context, retaining model and gamut evidence."""
from contextlib import contextmanager,ExitStack
from copy import deepcopy
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.restoration import DenoiseParams,DenoiseReport
from photo_worker.operations.image.prepare_denoise import prepare_denoise
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure
from .render_adjustments import AdjustmentPixels
from .render_resources import admit_render


@dataclass(frozen=True)
class RenderDenoiseStage:
    pixels: AdjustmentPixels
    report: DenoiseReport
    review_required: bool


def _same_denoise_pixels(actual,expected,check_active):
    """Compare exact float bits without full-width comparison temporaries."""
    if (not isinstance(actual,np.ndarray) or not isinstance(expected,np.ndarray)
            or actual.dtype!=np.float32 or expected.dtype!=np.float32
            or actual.shape!=expected.shape or actual.ndim not in (2,3)):
        raise ValueError('RENDER_DENOISE_CURRENT_PIXELS_CHANGED')
    h,w=expected.shape[:2]
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active()
            if not np.array_equal(actual[y:y+256,x:x+256].view(np.uint32),
                                  expected[y:y+256,x:x+256].view(np.uint32)):
                raise ValueError('RENDER_DENOISE_CURRENT_PIXELS_CHANGED')


@contextmanager
def prepare_pinned_denoise(artifacts,params_ref,current,pool,*,backend,execution_profile,
                          provider_peak,verify_runtime,expected_params=None,check_active=lambda:None):
    """Installer supplies qualified model peak; no model fallback or publication."""
    expected_document=None if expected_params is None else canonical_bytes(expected_params)
    params_ref=deepcopy(params_ref);profile=deepcopy(execution_profile);peak=deepcopy(provider_peak)
    if backend not in ('cpu','cuda','mps') or profile.get('backend')!=backend or not callable(verify_runtime):
        raise OperationFailure('CONFIG_INVALID')
    check_active();verify_runtime();check_active()
    def owned(ref,model):
        check_active();return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
    params=owned(params_ref,DenoiseParams)
    if expected_document is not None and canonical_bytes(params)!=expected_document:
        raise ValueError('RENDER_DENOISE_DOCUMENT_CHANGED')
    image=owned(params.image,WorkingImage)
    if params.model_lock_id!=pool.lock.lock_id:raise OperationFailure('MODEL_LOCK_MISMATCH')
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24':
        raise ValueError('RENDER_DENOISE_SOURCE_INVALID')
    h,w=image.canvas.height,image.canvas.width
    # Validate all caller model costs before augmenting retained image/halo costs.
    from .render_resources import render_resources
    render_resources(w,h,mask_refs=0,mask_nodes=0,provider_peak=peak)
    tile=min(h,params.tile+2*params.overlap)*min(w,params.tile+2*params.overlap)
    peak['ram_bytes']+=40*h*w+192*tile+1048576
    peak['scratch_bytes']+=40*h*w+1048576
    admit_render(profile,w,h,mask_refs=0,mask_nodes=0,provider_peak=peak)
    with ExitStack() as stack:
        def mapped(ref,shape):
            local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            return stack.enter_context(read_array(local.path,shape,'float32',check_active,unit_interval=True))
        def same(actual,expected):
            _same_denoise_pixels(actual,expected,check_active)
        same(current.rgb,mapped(image.pixels,(h,w,3)))
        if image.alpha is None:
            if current.alpha is not None:raise ValueError('RENDER_DENOISE_CURRENT_PIXELS_CHANGED')
        else:same(current.alpha,mapped(image.alpha,(h,w)))
        check_active()
        with pool.acquire(params.model_id,backend) as engine:
            with prepare_denoise(current.rgb,params,engine,artifacts.scratch,check_active=check_active) as prepared:
                with read_array(prepared.path,(h,w,3),'float32',check_active,unit_interval=True) as pixels:
                    warnings=[]
                    if prepared.source_clip:warnings.append('source_srgb_clipped')
                    if prepared.gamut_excess:warnings.append('delta_gamut_excess')
                    count=h*w
                    report=DenoiseReport(image=params.image,model_id=params.model_id,model_lock_id=params.model_lock_id,
                        backend=backend,color_adapter_id=params.color_adapter_id,strength=params.strength,tile=params.tile,
                        overlap=params.overlap,pixel_count=count,disabled_delta_fraction=prepared.disabled/count,
                        source_clip_fraction=prepared.source_clip/count,gamut_excess_fraction=prepared.gamut_excess/count,
                        alpha_preserved=image.alpha is not None,warnings=warnings)
                    yield RenderDenoiseStage(AdjustmentPixels(pixels,current.alpha,current.generated),report,bool(warnings))
                    check_active()


class PinnedDenoiseLayerProposal:
    """Task-local image.denoise@1 callback with explicit model resource peak."""
    def __init__(self,artifacts,pool,*,backend,execution_profile,provider_peak,runtime_sha256,
                 verify_runtime,check_active=lambda:None):
        import re
        if not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None:
            raise ValueError('RENDER_DENOISE_RUNTIME_INVALID')
        self.artifacts=artifacts;self.pool=pool;self.backend=backend
        self.profile=deepcopy(execution_profile);self.peak=deepcopy(provider_peak)
        self.runtime_sha256=runtime_sha256;self.verify_runtime=verify_runtime;self.check_active=check_active

    @contextmanager
    def __call__(self,item,current):
        from .render_layer_params import LoadedRenderLayer
        from .render_adjustments import RenderLayerProposal
        from .render_effects import RenderLayerEffect,effect_review_required
        item=deepcopy(item);self.check_active()
        if (not isinstance(item,LoadedRenderLayer) or item.layer.operation!='image.denoise'
                or item.layer.operation_version!='1' or item.runtime_sha256!=self.runtime_sha256
                or not isinstance(item.params,DenoiseParams)):
            raise ValueError('RENDER_DENOISE_LAYER_BINDING_INVALID')
        if canonical_bytes(self.artifacts.contract(item.layer.params,DenoiseParams))!=canonical_bytes(item.params):
            raise ValueError('RENDER_DENOISE_DOCUMENT_CHANGED')
        with prepare_pinned_denoise(self.artifacts,item.layer.params,current,self.pool,backend=self.backend,
                execution_profile=self.profile,provider_peak=self.peak,verify_runtime=self.verify_runtime,
                expected_params=item.params,check_active=self.check_active) as stage:
            effect=RenderLayerEffect(item.layer.id,'image.denoise','1',item.runtime_sha256,(),
                stage.review_required,denoise_report=deepcopy(stage.report))
            effect_review_required(effect)
            yield RenderLayerProposal(stage.pixels,effect)
            self.check_active()


class PinnedDenoiseProviderFactory:
    """Concrete RenderProviderAssembly layer factory for image.denoise@1.

    Installer supplies source-specific binding validation and qualified model
    costs. The factory adds model-lock validation and never invents a fallback.
    """
    def __init__(self,pool,*,backend,provider_peak,runtime_sha256,verify_runtime,validate_binding):
        import re
        if (backend not in ('cpu','cuda','mps') or not callable(verify_runtime)
                or not callable(validate_binding) or not isinstance(runtime_sha256,str)
                or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None):
            raise ValueError('RENDER_DENOISE_RUNTIME_INVALID')
        from .render_resources import render_resources
        render_resources(1,1,mask_refs=0,mask_nodes=0,provider_peak=provider_peak)
        self.pool=pool;self.backend=backend;self._peak=tuple(provider_peak.items())
        self.runtime_sha256=runtime_sha256
        self.verify_runtime=verify_runtime;self.validate_binding=validate_binding

    def __call__(self,artifacts,inputs,profile,check_active):
        from .render_layer_params import RenderLayerOperator
        from .render_providers import RenderLayerProvider
        if (not isinstance(profile,dict) or profile.get('backend')!=self.backend
                or not callable(check_active)):
            raise OperationFailure('CONFIG_INVALID')
        check_active()
        def binding(params,state):
            check_active()
            if not isinstance(params,DenoiseParams) or params.model_lock_id!=self.pool.lock.lock_id:
                raise OperationFailure('MODEL_LOCK_MISMATCH')
            self.validate_binding(params,state)
            check_active()
        operator=RenderLayerOperator('image.denoise','1',self.runtime_sha256,DenoiseParams,
            self.verify_runtime,binding)
        proposal=PinnedDenoiseLayerProposal(artifacts,self.pool,backend=self.backend,
            execution_profile=deepcopy(profile),provider_peak=dict(self._peak),
            runtime_sha256=self.runtime_sha256,verify_runtime=self.verify_runtime,check_active=check_active)
        return (RenderLayerProvider(operator,proposal),)


@contextmanager
def prepare_authored_denoise(artifacts,source_ref,restoration,params_ref,current,pool,*,
        backend,execution_profile,provider_peak,verify_runtime,check_active=lambda:None):
    """Execute the denoise stage bound to the effective authored restoration.

    Caller orders this before color/beauty and separately executes requested
    upscale/dust stages. This function does not claim complete restoration.
    """
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.style import Restoration
    if (not isinstance(source_ref,ArtifactRef) or not isinstance(params_ref,ArtifactRef)
            or not isinstance(restoration,Restoration)):
        raise ValueError('RENDER_DENOISE_AUTHORED_BINDING_INVALID')
    source_ref,params_ref=deepcopy((source_ref,params_ref))
    authored=Restoration.model_validate_json(canonical_bytes(restoration))
    profile,peak=deepcopy(execution_profile),deepcopy(provider_peak)
    check_active()
    params=DenoiseParams.model_validate_json(canonical_bytes(artifacts.contract(params_ref,DenoiseParams)))
    check_active()
    if (authored.denoise=='off' or params.image!=source_ref
            or params.model_id!=authored.denoise or params.strength!=authored.denoise_strength):
        raise ValueError('RENDER_DENOISE_AUTHORED_BINDING_INVALID')
    with prepare_pinned_denoise(artifacts,params_ref,current,pool,backend=backend,
            execution_profile=profile,provider_peak=peak,verify_runtime=verify_runtime,expected_params=params,
            check_active=check_active) as result:
        yield result
        check_active()

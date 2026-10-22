"""Pinned same-canvas portrait detail; source/model authority is external."""
from contextlib import contextmanager,ExitStack
from copy import deepcopy
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.portrait_detail import PortraitParams,DetailEvidence
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.algorithms.effective import resolve_effective_params
from photo_worker.operations.portrait.prepare_detail import prepare_detail,detail_peak
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels
from .render_resources import admit_render


@dataclass(frozen=True)
class RenderDetailStage:
    pixels: AdjustmentPixels
    regions: tuple[DetailEvidence,...]
    review_required: bool


def _same_detail_pixels(actual,expected,check_active):
    """Exact source bits, bounded independently of full canvas width."""
    if (not isinstance(actual,np.ndarray) or not isinstance(expected,np.ndarray)
            or actual.dtype!=np.float32 or expected.dtype!=np.float32
            or actual.shape!=expected.shape or actual.ndim not in (2,3)):
        raise ValueError('RENDER_DETAIL_CURRENT_PIXELS_CHANGED')
    h,w=expected.shape[:2]
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active()
            if not np.array_equal(actual[y:y+256,x:x+256].view(np.uint32),
                                  expected[y:y+256,x:x+256].view(np.uint32)):
                raise ValueError('RENDER_DETAIL_CURRENT_PIXELS_CHANGED')


@contextmanager
def prepare_pinned_detail(artifacts,params_ref,current,*,execution_profile,expected_params=None,check_active=lambda:None):
    """Only render pixels exactly bound to declared Params.image, preserve alpha.

    The qualified installer supplies adopted explicit mask/model provenance.
    This context cannot infer regions or apply pre-color masks to changed RGB.
    """
    expected_document=None if expected_params is None else canonical_bytes(expected_params)
    params_ref=deepcopy(params_ref);profile=deepcopy(execution_profile)
    def owned(ref,model):
        check_active();return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
    params=owned(params_ref,PortraitParams)
    if expected_document is not None and canonical_bytes(params)!=expected_document:
        raise ValueError('RENDER_DETAIL_DOCUMENT_CHANGED')
    image=owned(params.image,WorkingImage)
    resolved=owned(params.resolved,ResolvedStyle)
    if resolved.mode=='skip':raise ValueError('STYLE_OOD_SKIP')
    if (resolved.params.image!=params.image or image.dtype!='float32'
            or image.color.profile_id!='work_rec2020_gamma24_v1'
            or any(mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain
                for mask in [*(region.mask for region in params.regions),*params.protected])):
        raise ValueError('RENDER_DETAIL_SOURCE_INVALID')
    h,w=image.canvas.height,image.canvas.width
    peak=detail_peak(w,h);peak.update(vram_bytes=0,output_bytes=0)
    # Retained pinned RGB, alpha, every mapped explicit mask and combined protection.
    retained=(20+4*(len(params.regions)+len(params.protected)))*h*w+1048576
    peak['ram_bytes']+=retained;peak['scratch_bytes']+=retained
    admit_render(profile,w,h,mask_refs=0,mask_nodes=0,provider_peak=peak)
    detail=resolve_effective_params(resolved).modules.extensions.portrait_detail
    with ExitStack() as stack:
        def mapped(ref,shape,unit=True):
            check_active();local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            return stack.enter_context(read_array(local.path,shape,'float32',check_active,unit_interval=unit))
        pinned=mapped(image.pixels,(h,w,3))
        def same(actual,expected):
            _same_detail_pixels(actual,expected,check_active)
        same(current.rgb,pinned)
        if image.alpha is None:
            if current.alpha is not None:raise ValueError('RENDER_DETAIL_CURRENT_PIXELS_CHANGED')
        else:
            from photo_worker.runtime.arrays import read_alpha_array
            from .render_defects import alpha_tile_matches
            local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'))
            pinned_alpha=stack.enter_context(
                read_alpha_array(local.path,(h,w),check_active)
                if profile.get('alpha_mode','require_float32')=='normalize_uint16'
                else read_array(local.path,(h,w),'float32',check_active,unit_interval=True))
            if (not isinstance(current.alpha,np.ndarray) or current.alpha.shape!=(h,w)
                    or current.alpha.dtype!=np.float32):
                raise ValueError('RENDER_DETAIL_CURRENT_PIXELS_CHANGED')
            for y in range(0,h,256):
                for x in range(0,w,256):
                    check_active()
                    if not alpha_tile_matches(current.alpha[y:y+256,x:x+256],pinned_alpha[y:y+256,x:x+256]):
                        raise ValueError('RENDER_DETAIL_CURRENT_PIXELS_CHANGED')
        if detail is None or not detail.enabled:
            check_active();yield RenderDetailStage(current,(),resolved.needs_review);check_active();return
        protection=np.zeros((h,w),np.float32)
        for mask in params.protected:np.maximum(protection,mapped(mask.artifact,(h,w)),out=protection)
        with prepare_detail(current.rgb,params.regions,detail,lambda ref:mapped(ref,(h,w)),protection,
                check_active=check_active) as prepared:
            review=resolved.needs_review or any(region.reason not in (None,'zero_strength') for region in prepared.regions)
            yield RenderDetailStage(AdjustmentPixels(prepared.rgb,current.alpha,current.generated),prepared.regions,review)
            check_active()


class PinnedDetailLayerProposal:
    """Qualified installer's task-local portrait.detail@1 proposal callback."""
    def __init__(self,artifacts,*,execution_profile,runtime_sha256,check_active=lambda:None):
        import re
        if not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None:
            raise ValueError('RENDER_DETAIL_RUNTIME_INVALID')
        self.artifacts=artifacts;self.profile=deepcopy(execution_profile)
        self.runtime_sha256=runtime_sha256;self.check_active=check_active

    @contextmanager
    def __call__(self,item,current):
        from .render_layer_params import LoadedRenderLayer
        from .render_adjustments import RenderLayerProposal
        from .render_effects import RenderLayerEffect,effect_review_required
        item=deepcopy(item);self.check_active()
        if (not isinstance(item,LoadedRenderLayer) or item.layer.operation!='portrait.detail'
                or item.layer.operation_version!='1' or item.runtime_sha256!=self.runtime_sha256
                or not isinstance(item.params,PortraitParams)):
            raise ValueError('RENDER_DETAIL_LAYER_BINDING_INVALID')
        if canonical_bytes(self.artifacts.contract(item.layer.params,PortraitParams))!=canonical_bytes(item.params):
            raise ValueError('RENDER_DETAIL_DOCUMENT_CHANGED')
        with prepare_pinned_detail(self.artifacts,item.layer.params,current,execution_profile=self.profile,
                expected_params=item.params,check_active=self.check_active) as stage:
            effect=RenderLayerEffect(item.layer.id,'portrait.detail','1',item.runtime_sha256,(),
                stage.review_required,stage.regions)
            effect_review_required(effect)
            yield RenderLayerProposal(stage.pixels,effect)
            self.check_active()


class PinnedDetailProviderFactory:
    """Install one portrait.detail operator/proposal with scoped mask binding.

    Source/mask/model provenance remains the installer's required validator;
    qualification remains the renderer's independent deployment gate.
    """
    def __init__(self,*,runtime_sha256,verify_runtime,validate_binding):
        import re
        if (not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None
                or not callable(verify_runtime) or not callable(validate_binding)):
            raise ValueError('RENDER_DETAIL_RUNTIME_INVALID')
        self.runtime_sha256=runtime_sha256
        self.verify_runtime=verify_runtime;self.validate_binding=validate_binding

    def __call__(self,artifacts,inputs,profile,check_active):
        from .render_layer_params import RenderLayerOperator
        from .render_providers import RenderLayerProvider
        if not isinstance(profile,dict) or not callable(check_active):
            raise ValueError('RENDER_DETAIL_RUNTIME_INVALID')
        check_active()
        def binding(params,state):
            check_active()
            if not isinstance(params,PortraitParams):raise ValueError('RENDER_DETAIL_LAYER_BINDING_INVALID')
            self.validate_binding(params,state)
            check_active()
        operator=RenderLayerOperator('portrait.detail','1',self.runtime_sha256,PortraitParams,
            self.verify_runtime,binding)
        proposal=PinnedDetailLayerProposal(artifacts,execution_profile=deepcopy(profile),
            runtime_sha256=self.runtime_sha256,check_active=check_active)
        return (RenderLayerProvider(operator,proposal),)

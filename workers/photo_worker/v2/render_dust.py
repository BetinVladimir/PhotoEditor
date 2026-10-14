"""Scoped native dust repair; review/source authority comes from admission."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from copy import deepcopy
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.dust import DustRepairParams,DustCandidates,DustRepairReport,accepted_dust_candidates
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.sensor.prepare_repair import prepare_dust_repair
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels
from .render_resources import admit_render
from .render_protection import preserve_render_protection


@dataclass(frozen=True)
class RenderDustStage:
    pixels: AdjustmentPixels
    report: DustRepairReport
    review_required: bool


@contextmanager
def prepare_pinned_dust(artifacts,params_ref,current,*,execution_profile,verify_runtime,
                        read_protected=None,tile_size=512,expected_params=None,check_active=lambda:None):
    """Caller authorizes the pinned review and verified protection tile reader.

    No pending candidate is automatically accepted. Geometric applied-pixel
    counts precede protection restoration, so protection forces final review.
    """
    expected_document=None if expected_params is None else canonical_bytes(expected_params)
    if not callable(verify_runtime):raise ValueError('RENDER_DUST_RUNTIME_INVALID')
    params_ref=deepcopy(params_ref);profile=deepcopy(execution_profile)
    check_active();verify_runtime();check_active()
    def owned(ref,model):
        check_active()
        return model.model_validate_json(canonical_bytes(artifacts.contract(ref,model)))
    params=owned(params_ref,DustRepairParams)
    if expected_document is not None and canonical_bytes(params)!=expected_document:
        raise ValueError('RENDER_DUST_DOCUMENT_CHANGED')
    image=owned(params.image,WorkingImage)
    proposal=None if params.proposal is None else owned(params.proposal,DustCandidates)
    selected=accepted_dust_candidates(params,proposal)
    if (image.dtype!='float32' or image.canvas.orientation!=1
            or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24'):
        raise ValueError('RENDER_DUST_SOURCE_INVALID')
    h,w=image.canvas.height,image.canvas.width;count=h*w
    # Retained source/result, OpenCV masks/channels and evidence temporaries.
    peak=dict(ram_bytes=128*count+512*1048576,scratch_bytes=40*count+32768,
              vram_bytes=0,output_bytes=0)
    admit_render(profile,w,h,mask_refs=0,mask_nodes=0,provider_peak=peak,tile_size=tile_size)
    with ExitStack() as stack:
        def mapped(ref,shape):
            local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
            return stack.enter_context(read_array(local.path,shape,'float32',check_active,unit_interval=True))
        def same(actual,expected):
            if not isinstance(actual,np.ndarray) or actual.dtype!=np.float32 or actual.shape!=expected.shape:
                raise ValueError('RENDER_DUST_CURRENT_PIXELS_CHANGED')
            for y in range(0,h,256):
                for x in range(0,w,256):
                    check_active()
                    if not np.array_equal(actual[y:y+256,x:x+256].view(np.uint32),expected[y:y+256,x:x+256].view(np.uint32)):
                        raise ValueError('RENDER_DUST_CURRENT_PIXELS_CHANGED')
        same(current.rgb,mapped(image.pixels,(h,w,3)))
        if image.alpha is None:
            if current.alpha is not None:raise ValueError('RENDER_DUST_CURRENT_PIXELS_CHANGED')
        else:same(current.alpha,mapped(image.alpha,(h,w)))
        evidence=()
        output=current
        if selected:
            prepared=stack.enter_context(prepare_dust_repair(current.rgb,proposal.raw_crop,selected,
                artifacts.scratch,check_active=check_active))
            output=AdjustmentPixels(stack.enter_context(read_array(prepared.path,(h,w,3),'float32',
                check_active,unit_interval=True)),current.alpha,current.generated)
            evidence=prepared.candidates
        report=DustRepairReport(image=params.image,proposal=params.proposal,
            review_revision=1 if params.review is None else params.review.revision,
            candidates=list(evidence),warnings=[] if selected else ['no_accepted_candidates'])
        with preserve_render_protection(artifacts,current,output,read_protected,
                tile_size=tile_size,check_active=check_active) as protected:
            yield RenderDustStage(protected,report,not selected or read_protected is not None)
            check_active()


class PinnedDustLayerProposal:
    """Reviewed sensor.dust_repair@1 with retained pure effect metadata."""
    def __init__(self,artifacts,*,execution_profile,runtime_sha256,verify_runtime,
                 read_protected=None,tile_size=512,check_active=lambda:None):
        import re
        if (not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None
                or not callable(verify_runtime)):
            raise ValueError('RENDER_DUST_RUNTIME_INVALID')
        self.artifacts=artifacts;self.profile=deepcopy(execution_profile)
        self.runtime_sha256=runtime_sha256;self.verify_runtime=verify_runtime
        self.read_protected=read_protected;self.tile_size=tile_size;self.check_active=check_active

    @contextmanager
    def __call__(self,item,current):
        from .render_layer_params import LoadedRenderLayer
        from .render_effects import RenderLayerEffect,effect_review_required
        from .render_adjustments import RenderLayerProposal
        item=deepcopy(item);self.check_active()
        if (not isinstance(item,LoadedRenderLayer) or item.layer.operation!='sensor.dust_repair'
                or item.layer.operation_version!='1' or item.runtime_sha256!=self.runtime_sha256
                or not isinstance(item.params,DustRepairParams)):
            raise ValueError('RENDER_DUST_LAYER_BINDING_INVALID')
        if canonical_bytes(self.artifacts.contract(item.layer.params,DustRepairParams))!=canonical_bytes(item.params):
            raise ValueError('RENDER_DUST_DOCUMENT_CHANGED')
        with prepare_pinned_dust(self.artifacts,item.layer.params,current,execution_profile=self.profile,
                verify_runtime=self.verify_runtime,read_protected=self.read_protected,
                tile_size=self.tile_size,expected_params=item.params,check_active=self.check_active) as stage:
            effect=RenderLayerEffect(item.layer.id,'sensor.dust_repair','1',self.runtime_sha256,(),
                stage.review_required,dust_report=deepcopy(stage.report))
            effect_review_required(effect)
            yield RenderLayerProposal(stage.pixels,effect)
            self.check_active()


class PinnedDustProviderFactory:
    """Shared assembly factory with explicit source/review/protection authority."""
    def __init__(self,*,runtime_sha256,verify_runtime,validate_binding,protection_reader):
        import re
        if (not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None
                or not all(callable(value) for value in (verify_runtime,validate_binding,protection_reader))):
            raise ValueError('RENDER_DUST_RUNTIME_INVALID')
        self.runtime_sha256=runtime_sha256;self.verify_runtime=verify_runtime
        self.validate_binding=validate_binding;self.protection_reader=protection_reader

    def __call__(self,artifacts,inputs,profile,check_active):
        from .render_layer_params import RenderLayerOperator
        from .render_providers import RenderLayerProvider
        if (not isinstance(profile,dict) or not callable(check_active)
                or type(profile.get('tile_size')) is not int or profile['tile_size'] not in (256,512,1024)):
            raise ValueError('RENDER_DUST_RUNTIME_INVALID')
        owned_profile=deepcopy(profile)
        check_active()
        protection=self.protection_reader(artifacts,deepcopy(inputs),check_active)
        check_active()
        if protection is not None and not callable(protection):raise ValueError('RENDER_PROTECTION_INPUT_INVALID')
        def binding(params,state):
            check_active()
            if not isinstance(params,DustRepairParams):raise ValueError('RENDER_DUST_LAYER_BINDING_INVALID')
            self.validate_binding(params,state)
            check_active()
        operator=RenderLayerOperator('sensor.dust_repair','1',self.runtime_sha256,DustRepairParams,
            self.verify_runtime,binding)
        proposal=PinnedDustLayerProposal(artifacts,execution_profile=owned_profile,
            runtime_sha256=self.runtime_sha256,verify_runtime=self.verify_runtime,
            read_protected=protection,tile_size=owned_profile['tile_size'],check_active=check_active)
        return (RenderLayerProvider(operator,proposal),)


@contextmanager
def prepare_authored_dust(artifacts,source_ref,restoration,params_ref,current,*,
        execution_profile,verify_runtime,read_protected=None,tile_size=512,
        check_active=lambda:None):
    """Bind reviewed dust repair to the authored restoration and stage source.

    Admission proves review authority. The caller owns restoration ordering;
    automatic policies cannot silently reuse the human-review implementation.
    """
    from photo_worker.contracts.base import ArtifactRef
    from photo_worker.contracts.style import Restoration
    if (not isinstance(source_ref,ArtifactRef) or not isinstance(params_ref,ArtifactRef)
            or not isinstance(restoration,Restoration)):
        raise ValueError('RENDER_DUST_AUTHORED_BINDING_INVALID')
    source_ref,params_ref=deepcopy((source_ref,params_ref))
    authored=Restoration.model_validate_json(canonical_bytes(restoration))
    profile=deepcopy(execution_profile)
    check_active()
    if authored.sensor_dust!='review':
        raise ValueError('RENDER_DUST_AUTHORED_BINDING_INVALID')
    params=DustRepairParams.model_validate_json(canonical_bytes(artifacts.contract(params_ref,DustRepairParams)))
    check_active()
    if params.image!=source_ref:
        raise ValueError('RENDER_DUST_AUTHORED_BINDING_INVALID')
    with prepare_pinned_dust(artifacts,params_ref,current,execution_profile=profile,
            verify_runtime=verify_runtime,expected_params=params,read_protected=read_protected,tile_size=tile_size,
            check_active=check_active) as result:
        yield result
        check_active()

"""Scoped review-bound existing defect repair; never infer accepted candidates."""
from contextlib import contextmanager
from dataclasses import dataclass
from photo_worker.contracts.defects import accepted_candidates
from photo_worker.operations.defects.prepare import prepare_repair,RepairEvidence
from photo_worker.runtime.arrays import read_array
from .render_adjustments import AdjustmentPixels


@dataclass(frozen=True)
class RenderDefectStage:
    pixels:AdjustmentPixels
    candidates:tuple[RepairEvidence,...]
    review_required:bool


def alpha_tile_matches(current,pinned):
    """Compare normalized opacity with its exact legacy or float32 source tile."""
    import numpy as np
    if (not isinstance(current,np.ndarray) or not isinstance(pinned,np.ndarray)
            or current.shape!=pinned.shape or current.dtype!=np.float32
            or pinned.dtype not in (np.uint16,np.float32)):
        return False
    actual=(pinned.astype(np.float32)/np.float32(65535)
            if pinned.dtype==np.uint16 else pinned)
    return np.array_equal(current.view(np.uint32),actual.view(np.uint32))


@contextmanager
def prepare_defect_stage(artifacts,image_ref,image,current,proposal_ref,proposal,review,labels,protected,*,
                         strength,execution_profile,protection_ref,review_ref,max_roi_pixels=4194304,check_active=lambda:None):
    """Caller proves actual current image pin, review ACL and qualified runtime.

    Existing accepted_candidates source/revision/candidate guards remain in force.
    This helper does not approve review, publish intermediate images or bind a
    proposal made for different post-geometry/post-beauty pixels.
    """
    check_active()
    from copy import deepcopy
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.defects import DefectCandidates,DefectReview
    # Freeze review selection and candidate geometry before artifact callbacks.
    image=WorkingImage.model_validate_json(canonical_bytes(image))
    proposal=DefectCandidates.model_validate_json(canonical_bytes(proposal))
    review=DefectReview.model_validate_json(canonical_bytes(review))
    image_ref,proposal_ref,protection_ref,review_ref=deepcopy((image_ref,proposal_ref,protection_ref,review_ref))
    execution_profile=deepcopy(execution_profile)
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':
        raise ValueError('RENDER_DEFECT_SOURCE_INVALID')
    selected=accepted_candidates(proposal_ref,proposal,review,image_ref,image)
    from .render_resources import admit_render
    peak=defect_provider_peak(image_ref,image,proposal_ref,proposal,review,max_roi_pixels=max_roi_pixels)
    admit_render(execution_profile,image.canvas.width,image.canvas.height,mask_refs=0,mask_nodes=0,provider_peak=peak)
    for reference,model,value in ((image_ref,WorkingImage,image),(proposal_ref,DefectCandidates,proposal),
            (review_ref,DefectReview,review)):
        check_active()
        if canonical_bytes(artifacts.contract(reference,model))!=canonical_bytes(value):
            raise ValueError('RENDER_DEFECT_DOCUMENT_CHANGED')
    import numpy as np
    from photo_worker.contracts.images import MaskRef
    mask=artifacts.contract(protection_ref,MaskRef)
    if (not mask.protected or mask.canvas!=image.canvas or mask.transform_chain!=image.transform_chain):
        raise ValueError('RENDER_DEFECT_PROTECTION_INVALID')
    with artifacts.input(mask.artifact,suffix='.npy') as local:
        with read_array(local.path,(image.canvas.height,image.canvas.width),'float32',check_active,unit_interval=True) as pinned:
            if protected.shape!=pinned.shape or protected.dtype!=np.float32:
                raise ValueError('RENDER_DEFECT_PROTECTION_CHANGED')
            for y in range(0,pinned.shape[0],512):
                for x in range(0,pinned.shape[1],512):
                    check_active();part=np.s_[y:y+512,x:x+512]
                    if not np.array_equal(protected[part].view(np.uint32),pinned[part].view(np.uint32)):
                        raise ValueError('RENDER_DEFECT_PROTECTION_CHANGED')
    with artifacts.input(proposal.label_map,suffix='.npy') as local:
        with read_array(local.path,(image.canvas.height,image.canvas.width),'uint16',check_active) as pinned:
            if labels.shape!=pinned.shape or labels.dtype!=np.uint16:
                raise ValueError('RENDER_DEFECT_LABEL_MAP_CHANGED')
            for y in range(0,pinned.shape[0],512):
                for x in range(0,pinned.shape[1],512):
                    check_active();part=np.s_[y:y+512,x:x+512]
                    if not np.array_equal(labels[part],pinned[part]):
                        raise ValueError('RENDER_DEFECT_LABEL_MAP_CHANGED')
    with artifacts.input(image.pixels,suffix='.npy') as local:
        with read_array(local.path,(image.canvas.height,image.canvas.width,3),'float32',check_active,unit_interval=True) as pinned:
            if current.rgb.shape!=pinned.shape or current.rgb.dtype!=np.float32:
                raise ValueError('RENDER_DEFECT_CURRENT_PIXELS_CHANGED')
            for y in range(0,pinned.shape[0],512):
                for x in range(0,pinned.shape[1],512):
                    check_active();part=np.s_[y:y+512,x:x+512]
                    if not np.array_equal(current.rgb[part].view(np.uint32),pinned[part].view(np.uint32)):
                        raise ValueError('RENDER_DEFECT_CURRENT_PIXELS_CHANGED')
    if image.alpha is None:
        if current.alpha is not None:
            raise ValueError('RENDER_DEFECT_CURRENT_ALPHA_CHANGED')
    else:
        from photo_worker.runtime.arrays import read_alpha_array
        with artifacts.input(image.alpha,suffix='.npy') as local:
            with read_alpha_array(local.path,(image.canvas.height,image.canvas.width),check_active) as pinned:
                if current.alpha is None or current.alpha.shape!=pinned.shape or current.alpha.dtype!=np.float32:
                    raise ValueError('RENDER_DEFECT_CURRENT_ALPHA_CHANGED')
                for y in range(0,pinned.shape[0],512):
                    for x in range(0,pinned.shape[1],512):
                        check_active();part=np.s_[y:y+512,x:x+512]
                        if not alpha_tile_matches(current.alpha[part],pinned[part]):
                            raise ValueError('RENDER_DEFECT_CURRENT_ALPHA_CHANGED')
    with prepare_repair(current.rgb,labels,protected,proposal,selected,artifacts.scratch,
            strength=strength,max_roi_pixels=max_roi_pixels,check_active=check_active) as files:
        with read_array(files.image,(image.canvas.height,image.canvas.width,3),'float32',check_active) as rgb:
            evidence=files.candidates
            needs_review=not selected or any(item.reason is not None for item in evidence)
            yield RenderDefectStage(AdjustmentPixels(rgb,current.alpha,current.generated),evidence,needs_review)
            check_active()


def render_defect_params(prepared_style):
    """Derive owned effective repair params from materialized authored style."""
    from photo_worker.algorithms.effective import effective_defect_params
    return effective_defect_params(prepared_style.effective.defects,prepared_style.effective.strength)


def defect_provider_peak(image_ref,image,proposal_ref,proposal,review,*,max_roi_pixels=4194304):
    """Size existing repair support windows only after exact review binding."""
    import math
    if type(max_roi_pixels) is not int or not 1<=max_roi_pixels<=4194304:
        raise ValueError('RENDER_DEFECT_ROI_BUDGET_INVALID')
    selected=accepted_candidates(proposal_ref,proposal,review,image_ref,image)
    h,w=image.canvas.height,image.canvas.width
    if h*w>200000000:raise ValueError('RENDER_DEFECT_PIXEL_LIMIT')
    peak_roi=0
    for candidate in selected:
        radius=min(5.,max(1.,.25*candidate.diameter));halo=max(16,math.ceil(4*radius)+1)
        rect=candidate.bbox
        width=min(w,rect.x+rect.width+halo)-max(0,rect.x-halo)
        height=min(h,rect.y+rect.height+halo)-max(0,rect.y-halo)
        pixels=width*height
        if pixels>max_roi_pixels:raise ValueError('REPAIR_ROI_BUDGET_EXCEEDED')
        peak_roi=max(peak_roi,pixels)
    return dict(ram_bytes=(36+(4 if image.alpha is not None else 0))*h*w+256*peak_roi+128*1024*1024,
        scratch_bytes=30*h*w+32768,vram_bytes=0,output_bytes=0)


@contextmanager
def prepare_pinned_defect_repair(artifacts,params_ref,current,*,protection_ref,
        execution_profile,max_roi_pixels=4194304,expected_params=None,check_active=lambda:None):
    """Execute declared repair Params with scoped labels/protection and review.

    Qualified render_proposal installers may use this context for defects.repair.
    Caller proves operation/runtime/source authority and retains returned repair
    outcomes for mandatory final measurement/review; this does not approve review.
    """
    from contextlib import ExitStack
    from copy import deepcopy
    from dataclasses import replace
    from photo_worker.contracts.base import ArtifactRef,canonical_bytes
    from photo_worker.contracts.defects import DefectRepairParams,DefectCandidates,DefectReview
    from photo_worker.contracts.images import WorkingImage,MaskRef
    from photo_worker.contracts.resolved import ResolvedStyle
    from photo_worker.algorithms.effective import resolve_effective_params
    from .render_resources import admit_render
    expected_document=None if expected_params is None else canonical_bytes(expected_params)
    check_active()
    if not isinstance(params_ref,ArtifactRef) or not isinstance(protection_ref,ArtifactRef):
        raise ValueError('RENDER_DEFECT_PARAMS_REF_INVALID')
    params_ref,protection_ref=deepcopy((params_ref,protection_ref))
    execution_profile=deepcopy(execution_profile)
    def owned(reference,model):
        check_active()
        return model.model_validate_json(canonical_bytes(artifacts.contract(reference,model)))
    params=owned(params_ref,DefectRepairParams)
    if expected_document is not None and canonical_bytes(params)!=expected_document:
        raise ValueError('RENDER_DEFECT_DOCUMENT_CHANGED')
    image=owned(params.image,WorkingImage)
    proposal=owned(params.proposal,DefectCandidates)
    review=owned(params.review,DefectReview)
    resolved=owned(params.resolved,ResolvedStyle)
    if resolved.mode=='skip':raise ValueError('STYLE_OOD_SKIP')
    source=image if resolved.params.image==params.image else owned(resolved.params.image,WorkingImage)
    mask=owned(protection_ref,MaskRef)
    if (image.color!=source.color or image.canvas.source_sha256!=source.canvas.source_sha256
            or canonical_bytes(mask)!=canonical_bytes(params.protected)):
        raise ValueError('RENDER_DEFECT_SOURCE_INVALID')
    effective=resolve_effective_params(resolved).modules.defects
    peak=defect_provider_peak(params.image,image,params.proposal,proposal,review,max_roi_pixels=max_roi_pixels)
    pixels=image.canvas.width*image.canvas.height
    peak['ram_bytes']+=6*pixels+16384
    peak['scratch_bytes']+=6*pixels+16384
    admit_render(execution_profile,image.canvas.width,image.canvas.height,mask_refs=0,mask_nodes=0,provider_peak=peak)
    # Even a disabled resolved module must prove the exact reviewed current
    # pixels; zero-strength native repair preserves that binding and evidence.
    strength=effective.strength if effective.enabled else 0.
    with ExitStack() as stack:
        def mapped(reference,dtype,unit):
            check_active()
            local=stack.enter_context(artifacts.input(reference,suffix='.npy'))
            return stack.enter_context(read_array(local.path,(image.canvas.height,image.canvas.width),
                dtype,check_active,unit_interval=unit))
        labels=mapped(proposal.label_map,'uint16',False)
        protected=mapped(mask.artifact,'float32',True)
        with prepare_defect_stage(artifacts,params.image,image,current,params.proposal,proposal,review,
                labels,protected,strength=strength,protection_ref=protection_ref,
                review_ref=params.review,execution_profile=execution_profile,max_roi_pixels=max_roi_pixels,
                check_active=check_active) as result:
            yield replace(result,review_required=result.review_required or resolved.needs_review)
            check_active()


class PinnedDefectLayerProposal:
    """Task-local render_proposal callback; runtime qualification is external."""
    def __init__(self,artifacts,*,protection_ref,execution_profile,runtime_sha256,
                 max_roi_pixels=4194304,check_active=lambda:None):
        from copy import deepcopy
        import re
        if not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None:
            raise ValueError('RENDER_DEFECT_RUNTIME_INVALID')
        self.artifacts=artifacts;self.protection_ref=deepcopy(protection_ref)
        self.execution_profile=deepcopy(execution_profile);self.runtime_sha256=runtime_sha256
        self.max_roi_pixels=max_roi_pixels;self.check_active=check_active

    @contextmanager
    def __call__(self,item,current):
        from photo_worker.contracts.base import canonical_bytes
        from photo_worker.contracts.defects import DefectRepairParams
        from .render_layer_params import LoadedRenderLayer
        from .render_adjustments import RenderLayerProposal
        from .render_effects import RenderLayerEffect,effect_review_required
        from copy import deepcopy
        item=deepcopy(item)
        self.check_active()
        if (not isinstance(item,LoadedRenderLayer) or item.layer.operation!='defects.repair'
                or item.layer.operation_version!='1' or item.runtime_sha256!=self.runtime_sha256
                or not isinstance(item.params,DefectRepairParams)):
            raise ValueError('RENDER_DEFECT_LAYER_BINDING_INVALID')
        if canonical_bytes(self.artifacts.contract(item.layer.params,DefectRepairParams))!=canonical_bytes(item.params):
            raise ValueError('RENDER_DEFECT_DOCUMENT_CHANGED')
        with prepare_pinned_defect_repair(self.artifacts,item.layer.params,current,
                protection_ref=self.protection_ref,execution_profile=self.execution_profile,
                max_roi_pixels=self.max_roi_pixels,expected_params=item.params,check_active=self.check_active) as stage:
            outcome=RenderLayerEffect(item.layer.id,item.layer.operation,item.layer.operation_version,
                item.runtime_sha256,stage.candidates,stage.review_required)
            effect_review_required(outcome)
            yield RenderLayerProposal(stage.pixels,outcome)
            self.check_active()


class PinnedDefectProviderFactory:
    """Install reviewed defect repair with an explicit scoped protection pin.

    The installer must resolve a declared protection document and validate the
    source/review authority. This factory never selects or accepts candidates.
    """
    def __init__(self,*,runtime_sha256,verify_runtime,validate_binding,protection_reference,
                 max_roi_pixels=4194304):
        import re
        if (not isinstance(runtime_sha256,str) or re.fullmatch('[0-9a-f]{64}',runtime_sha256) is None
                or not all(callable(value) for value in (verify_runtime,validate_binding,protection_reference))
                or type(max_roi_pixels) is not int or not 1<=max_roi_pixels<=4194304):
            raise ValueError('RENDER_DEFECT_RUNTIME_INVALID')
        self.runtime_sha256=runtime_sha256;self.verify_runtime=verify_runtime
        self.validate_binding=validate_binding;self.protection_reference=protection_reference
        self.max_roi_pixels=max_roi_pixels

    def __call__(self,artifacts,inputs,profile,check_active):
        from copy import deepcopy
        from photo_worker.contracts.base import ArtifactRef
        from photo_worker.contracts.defects import DefectRepairParams
        from .render_layer_params import RenderLayerOperator
        from .render_providers import RenderLayerProvider
        if not isinstance(profile,dict) or not callable(check_active):
            raise ValueError('RENDER_DEFECT_RUNTIME_INVALID')
        check_active()
        owned_profile=deepcopy(profile)
        protection=deepcopy(self.protection_reference(deepcopy(inputs)))
        check_active()
        if not isinstance(protection,ArtifactRef):raise ValueError('RENDER_DEFECT_PARAMS_REF_INVALID')
        def binding(params,state):
            check_active()
            if not isinstance(params,DefectRepairParams):raise ValueError('RENDER_DEFECT_LAYER_BINDING_INVALID')
            self.validate_binding(params,state)
            check_active()
        operator=RenderLayerOperator('defects.repair','1',self.runtime_sha256,DefectRepairParams,
            self.verify_runtime,binding)
        proposal=PinnedDefectLayerProposal(artifacts,protection_ref=protection,
            execution_profile=owned_profile,runtime_sha256=self.runtime_sha256,
            max_roi_pixels=self.max_roi_pixels,check_active=check_active)
        return (RenderLayerProvider(operator,proposal),)

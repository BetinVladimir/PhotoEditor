"""Ordered color and beauty building block with retained effect evidence."""
from contextlib import contextmanager
from dataclasses import dataclass,replace
from photo_worker.contracts.style import Disabled,StyleV2
from .render_color import COLOR_MODULES,prepare_color_base
from .render_beauty import prepare_beauty_stage,render_beauty_params
from .render_modules import required_render_modules
from .render_adjustments import AdjustmentPixels
from .render_protection import preserve_render_protection
from photo_worker.operations.portrait.prepare import FaceEvidence

PORTRAIT_MODULES=COLOR_MODULES|frozenset({'beauty'})


@dataclass(frozen=True)
class RenderPortraitBase:
    pixels:AdjustmentPixels
    beauty_evidence:tuple[FaceEvidence,...]
    review_required:bool


@contextmanager
def prepare_portrait_base(artifacts,inputs,style,original,masks,*,faces,person_reliable,
                          execution_profile,permanent=None,features=None,coverage_counts=None,person_mask=None,
                          region_masks=None,tile_size=512,max_roi_pixels=4194304,check_active=lambda:None):
    """Execute color then beauty, never waive unsupported authored stages.

    Caller verifies source/face/mask authority and native/model qualification.
    This returns effect evidence for mandatory downstream quality measurement;
    it neither publishes a report nor claims complete remaining effect support.
    """
    check_active()
    if not required_render_modules(style.effective)<=PORTRAIT_MODULES:
        raise ValueError('RENDER_PORTRAIT_EFFECT_PIPELINE_REQUIRED')
    from .render_prepare import admit_render_inputs
    peak=portrait_provider_peak(inputs.source,style,faces,has_protection=inputs.protection is not None,
        tile_size=tile_size,max_roi_pixels=max_roi_pixels)
    admit_render_inputs(inputs,execution_profile,peak,tile_size=tile_size)
    color_style=style
    if style.effective.beauty.enabled:
        data=style.effective.model_dump(mode='python',by_alias=True)
        data['beauty']=Disabled().model_dump(mode='python')
        color_style=replace(style,effective=StyleV2.model_validate(data))
    with prepare_color_base(artifacts,inputs,color_style,original,person_reliable=person_reliable,
            features=features,coverage_counts=coverage_counts,person_mask=person_mask,region_masks=region_masks,
            read_protected=masks.protection if inputs.protection is not None else None,
            tile_size=tile_size,check_active=check_active) as colored:
        if not style.effective.beauty.enabled:
            yield RenderPortraitBase(colored,(),style.effective.quality.status=='needs_review')
        else:
            with prepare_beauty_stage(artifacts,inputs.source,colored,render_beauty_params(style),faces,
                    permanent=permanent,tile_size=tile_size,max_roi_pixels=max_roi_pixels,
                    check_active=check_active) as beauty:
                with preserve_render_protection(artifacts,original,beauty.pixels,
                        masks.protection if inputs.protection is not None else None,
                        tile_size=tile_size,check_active=check_active) as protected:
                    yield RenderPortraitBase(protected,beauty.faces,beauty.review_required or style.effective.quality.status=='needs_review')
        check_active()


def portrait_provider_peak(source, style, faces, *, has_protection, tile_size=512,max_roi_pixels=4194304):
    """Bound retained color/beauty/protection owners and actual planned face ROIs."""
    from .render_color import color_provider_peak
    from photo_worker.operations.portrait.prepare import FaceInput
    from photo_worker.algorithms.beauty_roi import plan_roi
    if (type(max_roi_pixels) is not int or not 1<=max_roi_pixels<=4194304
            or type(has_protection) is not bool or not isinstance(faces,tuple) or len(faces)>64
            or any(not isinstance(face,FaceInput) for face in faces)
            or len({face.face.object_id for face in faces})!=len(faces)
            or not required_render_modules(style.effective)<=PORTRAIT_MODULES):
        raise ValueError('RENDER_PORTRAIT_RESOURCES_INVALID')
    value=style.effective
    data=value.model_dump(mode='python',by_alias=True);data['beauty']=Disabled().model_dump(mode='python')
    peak=color_provider_peak(source.canvas.width,source.canvas.height,StyleV2.model_validate(data),tile_size=tile_size)
    if not value.beauty.enabled:
        if faces:raise ValueError('RENDER_PORTRAIT_UNEXPECTED_FACES')
        return peak
    roi_pixels=[]
    for entry in faces:
        if entry.face.canvas!=source.canvas or entry.face.transform_chain!=source.transform_chain:
            raise ValueError('RENDER_PORTRAIT_FACE_SOURCE_CHANGED')
        plan=plan_roi(entry.face,max_work_pixels=max_roi_pixels)
        roi_pixels.append(plan.rect.width*plan.rect.height)
    pixels=source.canvas.width*source.canvas.height
    beauty_scratch=16*pixels+16*sum(roi_pixels)+8192*(2+2*len(faces))
    protection_scratch=16*pixels+16384 if has_protection else 0
    # Keep native masks and mapped pages resident conservatively; ROI filter
    # working peak is separate from retained delta/weight files.
    peak['scratch_bytes']+=beauty_scratch+protection_scratch
    peak['ram_bytes']+=beauty_scratch+protection_scratch+8*len(faces)*pixels+256*max(roi_pixels,default=0)
    if any(value>2**53-1 for value in peak.values()):raise ValueError('RENDER_PORTRAIT_RESOURCES_OVERFLOW')
    return peak


@contextmanager
def prepare_denoised_portrait_base(artifacts,inputs,style,original,masks,*,params_ref,pool,
        backend,provider_peak,verify_runtime,faces,person_reliable,execution_profile,
        permanent=None,features=None,coverage_counts=None,person_mask=None,region_masks=None,
        tile_size=512,max_roi_pixels=4194304,check_active=lambda:None):
    """Authored denoise → color → beauty with all stage owners held through yield.

    Installer supplies scoped source/model evidence and qualified model costs.
    Upscale and dust require their geometry/review-aware orchestration; they
    cannot be silently omitted by this same-canvas portrait pipeline.
    """
    from copy import deepcopy
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.restoration import DenoiseParams
    from .render_denoise import prepare_authored_denoise
    from .render_prepare import admit_render_inputs
    from .render_effects import RenderEffectChain
    check_active()
    inputs=deepcopy(inputs);style=deepcopy(style);profile=deepcopy(execution_profile)
    model_peak=deepcopy(provider_peak);peak=deepcopy(model_peak);params_ref=deepcopy(params_ref)
    restoration=style.effective.extensions.restoration
    if (restoration is None or restoration.denoise=='off' or restoration.upscale!='off'
            or restoration.sensor_dust!='off'
            or not required_render_modules(style.effective)<=PORTRAIT_MODULES|{'restoration'}):
        raise ValueError('RENDER_COMPLETE_RESTORATION_REQUIRED')
    data=style.effective.model_dump(mode='python',by_alias=True)
    data['extensions']['restoration']=None
    portrait_style=replace(style,effective=StyleV2.model_validate(data))
    params=DenoiseParams.model_validate_json(canonical_bytes(artifacts.contract(params_ref,DenoiseParams)))
    check_active()
    source_ref=inputs.recipe.recipe.source.source
    if params.image!=source_ref or params.model_id!=restoration.denoise or params.strength!=restoration.denoise_strength:
        raise ValueError('RENDER_DENOISE_AUTHORED_BINDING_INVALID')
    peak=denoised_portrait_provider_peak(inputs.source,style,params,faces,
        provider_peak=model_peak,has_protection=inputs.protection is not None,
        tile_size=tile_size,max_roi_pixels=max_roi_pixels)
    admit_render_inputs(inputs,profile,peak,tile_size=tile_size)
    # The first Params snapshot also remains authoritative through the nested
    # authored adapter's metadata read, not just the final native read.
    class BoundArtifacts:
        def __getattr__(self,name):return getattr(artifacts,name)
        def contract(self,reference,model):
            value=artifacts.contract(reference,model)
            if reference==params_ref and canonical_bytes(value)!=canonical_bytes(params):
                raise ValueError('RENDER_DENOISE_DOCUMENT_CHANGED')
            return value
    with prepare_authored_denoise(BoundArtifacts(),source_ref,restoration,params_ref,original,pool,
            backend=backend,execution_profile=profile,provider_peak=model_peak,
            verify_runtime=verify_runtime,check_active=check_active) as denoised:
        with prepare_portrait_base(artifacts,inputs,portrait_style,denoised.pixels,masks,
                faces=faces,person_reliable=person_reliable,execution_profile=profile,
                permanent=permanent,features=features,coverage_counts=coverage_counts,
                person_mask=person_mask,region_masks=region_masks,tile_size=tile_size,
                max_roi_pixels=max_roi_pixels,check_active=check_active) as portrait:
            with preserve_render_protection(artifacts,original,portrait.pixels,
                    masks.protection if inputs.protection is not None else None,
                    tile_size=tile_size,check_active=check_active) as protected:
                yield RenderEffectChain(protected,(denoised,portrait))
                check_active()


def denoised_portrait_provider_peak(source,style,params,faces,*,provider_peak,has_protection,
        tile_size=512,max_roi_pixels=4194304):
    """Complete retained peak usable by the installer before source pixel IO."""
    from copy import deepcopy
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.restoration import DenoiseParams
    from .render_resources import render_resources
    params=DenoiseParams.model_validate_json(canonical_bytes(params))
    style=deepcopy(style);peak=deepcopy(provider_peak)
    restoration=style.effective.extensions.restoration
    if (restoration is None or restoration.denoise=='off' or restoration.upscale!='off'
            or restoration.sensor_dust!='off' or params.model_id!=restoration.denoise
            or params.strength!=restoration.denoise_strength
            or not required_render_modules(style.effective)<=PORTRAIT_MODULES|{'restoration'}):
        raise ValueError('RENDER_COMPLETE_RESTORATION_REQUIRED')
    data=style.effective.model_dump(mode='python',by_alias=True)
    data['extensions']['restoration']=None
    portrait_style=replace(style,effective=StyleV2.model_validate(data))
    h,w=source.canvas.height,source.canvas.width
    render_resources(w,h,mask_refs=0,mask_nodes=0,provider_peak=peak)
    tile=min(h,params.tile+2*params.overlap)*min(w,params.tile+2*params.overlap)
    peak['ram_bytes']+=40*h*w+192*tile+1048576
    peak['scratch_bytes']+=40*h*w+1048576
    portrait_peak=portrait_provider_peak(source,portrait_style,faces,
        has_protection=has_protection,tile_size=tile_size,max_roi_pixels=max_roi_pixels)
    for key,value in portrait_peak.items():peak[key]+=value
    if has_protection:
        for key in ('ram_bytes','scratch_bytes'):peak[key]+=16*h*w+16384
    render_resources(w,h,mask_refs=0,mask_nodes=0,provider_peak=peak)
    return peak

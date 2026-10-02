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

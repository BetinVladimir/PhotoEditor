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


@contextmanager
def prepare_defect_stage(artifacts,image_ref,image,current,proposal_ref,proposal,review,labels,protected,*,
                         strength,execution_profile,protection_ref,review_ref,max_roi_pixels=4194304,check_active=lambda:None):
    """Caller proves actual current image pin, review ACL and qualified runtime.

    Existing accepted_candidates source/revision/candidate guards remain in force.
    This helper does not approve review, publish intermediate images or bind a
    proposal made for different post-geometry/post-beauty pixels.
    """
    check_active()
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':
        raise ValueError('RENDER_DEFECT_SOURCE_INVALID')
    selected=accepted_candidates(proposal_ref,proposal,review,image_ref,image)
    from .render_resources import admit_render
    peak=defect_provider_peak(image_ref,image,proposal_ref,proposal,review,max_roi_pixels=max_roi_pixels)
    admit_render(execution_profile,image.canvas.width,image.canvas.height,mask_refs=0,mask_nodes=0,provider_peak=peak)
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.contracts.images import WorkingImage
    from photo_worker.contracts.defects import DefectCandidates,DefectReview
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
        with artifacts.input(image.alpha,suffix='.npy') as local:
            with read_array(local.path,(image.canvas.height,image.canvas.width),'float32',check_active,unit_interval=True) as pinned:
                if current.alpha is None or current.alpha.shape!=pinned.shape or current.alpha.dtype!=np.float32:
                    raise ValueError('RENDER_DEFECT_CURRENT_ALPHA_CHANGED')
                for y in range(0,pinned.shape[0],512):
                    for x in range(0,pinned.shape[1],512):
                        check_active();part=np.s_[y:y+512,x:x+512]
                        if not np.array_equal(current.alpha[part].view(np.uint32),pinned[part].view(np.uint32)):
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

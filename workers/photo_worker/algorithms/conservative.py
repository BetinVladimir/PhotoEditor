"""Fixed conservative_auto_v1 eligibility, independent of human decisions.

Heuristic scores are not probabilities. Only tiny, red, non-dark skin spots
with a strong DoG response qualify; pigmented marks are intentionally retained.
Any positive authored/permanent/feature protection excludes the entire patch.
Dust must recur in every one of >=10 sensor-aligned frames and avoid subjects.
These constants are part of the pinned engine, never user-tunable thresholds.
"""
import math
import numpy as np
from photo_worker.algorithms.defect_labels import validate_labels
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.defects import DefectCandidates
from photo_worker.contracts.dust import verify_dust_candidate_ids


def _unit(block):
    if block.dtype!=np.float32 or not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):
        raise ValueError('CONSERVATIVE_INPUT_INVALID')


def select_defects(proposal,rgb,labels,protected,*,check_active=lambda:None):
    proposal=DefectCandidates.model_validate_json(canonical_bytes(proposal))
    return select_defect_regions(proposal,rgb,labels,protected,check_active=check_active)


def select_defect_regions(proposal,rgb,labels,protected,*,check_active=lambda:None):
    """Same pinned thresholds for authenticated or fresh private inventories.

    The caller owns provenance. Private inventories contain no ArtifactRefs and
    must validate their complete label/geometry inventory through coherent().
    """
    h,w=proposal.canvas.height,proposal.canvas.width
    if rgb.shape!=(h,w,3) or protected.shape!=(h,w):raise ValueError('CONSERVATIVE_INPUT_INVALID')
    validate_labels(labels,proposal,check_active=check_active)
    selected=[]
    for c in sorted(proposal.candidates,key=lambda c:c.label):
        check_active();b=c.bbox
        # Restrict diameter to genuinely small sensor spots, never broad marks.
        if c.score<.9 or not 1<=c.diameter<=6 or b.width>8 or b.height>8:continue
        halo=16;x0,y0=b.x-halo,b.y-halo;x1,y1=b.x+b.width+halo,b.y+b.height+halo
        if x0<0 or y0<0 or x1>w or y1>h:continue
        sl=(slice(y0,y1),slice(x0,x1));patch=rgb[sl];guard=protected[sl];_unit(patch);_unit(guard)
        # Context itself must be unprotected: no borrowing nearby permanent marks.
        if np.any(guard>0):continue
        foreground=labels[sl]==c.label
        context=labels[sl]==0
        if np.count_nonzero(foreground)<3 or np.count_nonzero(context)<64:continue
        centre=np.median(patch[foreground],axis=0);surround=np.median(patch[context],axis=0)
        if np.max(np.std(patch[context],axis=0))>.06:continue
        weights=np.array([.2627,.6780,.0593],np.float32)
        # Never remove a dark/pigmented mole, freckle or blackhead automatically.
        if float(centre@weights)<float(surround@weights)-.01:continue
        red=float(centre[0]-.5*(centre[1]+centre[2]));base=float(surround[0]-.5*(surround[1]+surround[2]))
        if red-base<.04 or centre[0]-max(centre[1],centre[2])<.03:continue
        selected.append(c)
    return tuple(selected)


def select_dust(proposal,image_ref,image,protected,*,check_active=lambda:None):
    proposal=verify_dust_candidate_ids(proposal)
    if image_ref not in proposal.frames:raise ValueError('CONSERVATIVE_DUST_SOURCE_INVALID')
    h,w=image.canvas.height,image.canvas.width;crop=proposal.raw_crop
    if not protected or any(mask.shape!=(h,w) for mask in protected):raise ValueError('CONSERVATIVE_PROTECTION_REQUIRED')
    if len(proposal.frames)<10 or image.transform_chain.steps:return ()
    selected=[]
    for c in proposal.candidates:
        check_active()
        if c.observed_frames!=c.eligible_frames or c.eligible_frames!=len(proposal.frames) or c.recurrence!=1 or c.radius_sensor_px>2:continue
        x=(c.sensor_x-crop.x+.5)*w/crop.width-.5;y=(c.sensor_y-crop.y+.5)*h/crop.height-.5
        radius=max(1.,c.radius_sensor_px*.5*(w/crop.width+h/crop.height))
        if radius>2:continue
        halo=math.ceil(radius)+32;x0,y0=round(x)-halo,round(y)-halo;x1,y1=round(x)+halo+1,round(y)+halo+1
        if min(x0,y0)<0 or x1>w or y1>h:continue
        allowed=True
        for mask in protected:
            block=mask[y0:y1,x0:x1];_unit(block)
            if np.any(block>0):allowed=False;break
        if allowed:selected.append(c)
    return tuple(sorted(selected,key=lambda c:c.id))

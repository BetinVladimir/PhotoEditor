from contextlib import contextmanager
from dataclasses import dataclass
from collections.abc import Callable,Iterator,Sequence
from pathlib import Path
import math
import numpy as np
from photo_worker.contracts.defects import DefectCandidates,DefectCandidate
from photo_worker.algorithms.defect_labels import validate_labels
from photo_worker.algorithms.defect_repair import repair_accepted
from photo_worker.runtime.scratch import ScratchSpace,Reservation

@dataclass(frozen=True)
class RepairEvidence:
    candidate_id:str
    label:int
    applied_pixels:int
    reason:str|None
    radius:float
    below_zero:float=0.
    above_one:float=0.

@dataclass(frozen=True)
class RepairFiles:
    image:Path
    reservation:Reservation
    candidates:tuple[RepairEvidence,...]


@contextmanager
def prepare_repair(rgb:np.ndarray,labels:np.ndarray,protected:np.ndarray,proposal:DefectCandidates,
                   selected:Sequence[DefectCandidate],scratch:ScratchSpace,*,strength:float=1.,
                   max_roi_pixels:int=4194304,check_active:Callable[[],None]=lambda:None)->Iterator[RepairFiles]:
    """Caller must authorize selection through an owned review or pinned auto policy.

    This native pixel engine does not manufacture or validate review authority.

    Patches read only original RGB. Output writes only the selected label pixels,
    so overlapping support windows never undo or compound neighboring repairs.
    """
    check_active();validate_labels(labels,proposal,check_active=check_active)
    h,w=proposal.canvas.height,proposal.canvas.width
    if rgb.shape!=(h,w,3) or rgb.dtype!=np.float32 or protected.shape!=(h,w) or protected.dtype!=np.float32:
        raise ValueError('Invalid repair input layout')
    if not math.isfinite(strength) or not 0<=strength<=1 or type(max_roi_pixels) is not int or not 1<=max_roi_pixels<=4194304:
        raise ValueError('Invalid repair configuration')
    known={c.id:c for c in proposal.candidates}
    if len({c.id for c in selected})!=len(selected) or any(known.get(c.id)!=c for c in selected):
        raise ValueError('Repair selection does not match proposal')
    plans=[]
    for c in sorted(selected,key=lambda c:c.label):
        radius=min(5.,max(1.,.25*c.diameter));halo=max(16,math.ceil(4*radius)+1);r=c.bbox
        x0,y0=max(0,r.x-halo),max(0,r.y-halo);x1,y1=min(w,r.x+r.width+halo),min(h,r.y+r.height+halo)
        if (x1-x0)*(y1-y0)>max_roi_pixels:raise ValueError('REPAIR_ROI_BUDGET_EXCEEDED')
        plans.append((c,(slice(y0,y1),slice(x0,x1)),radius))
    with scratch.reserve(h*w*12+8192) as reservation:
        path=reservation.path('repaired-rgb','.npy')
        output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3))
        evidence=[]
        try:
            for y in range(0,h,256):
                for x in range(0,w,256):
                    check_active();sl=(slice(y,y+256),slice(x,x+256))
                    for source in (rgb[sl],protected[sl]):
                        if not np.isfinite(source).all() or np.any(source<0) or np.any(source>1):raise ValueError('Invalid repair values')
                    output[sl]=rgb[sl]
            for candidate,sl,radius in plans:
                check_active();accepted=labels[sl]==candidate.label
                try:
                    result=repair_accepted(rgb[sl],accepted,protected[sl],candidate.diameter,strength=strength,
                        max_work_pixels=max_roi_pixels,check_active=check_active)
                except ValueError as exc:
                    if str(exc)!='INSUFFICIENT_REPAIR_CONTEXT':raise
                    evidence.append(RepairEvidence(candidate.id,candidate.label,0,'insufficient_context',radius));continue
                output[sl][accepted]=result.rgb[accepted]
                reason=None if result.applied_pixels else 'zero_strength' if strength==0 else 'protected'
                evidence.append(RepairEvidence(candidate.id,candidate.label,result.applied_pixels,reason,radius,result.below_zero,result.above_one))
            output.flush();reservation.validate()
        finally:output._mmap.close()
        check_active()
        yield RepairFiles(path,reservation,tuple(evidence))

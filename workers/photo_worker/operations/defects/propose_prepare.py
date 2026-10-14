from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from collections.abc import Callable,Iterator,Sequence
from pathlib import Path
from uuid import UUID
import numpy as np
from photo_worker.contracts.images import WorkingImage,Rect
from .protection import plan_proposal_face
from photo_worker.algorithms.skin_mask import build_skin_mask
from photo_worker.algorithms.defect_propose import propose_defects,ProposedDefect
from photo_worker.algorithms.mask_png import png16_bound,write_png16
from photo_worker.runtime.scratch import ScratchSpace,Reservation
from photo_worker.operations.portrait.prepare import FaceInput

@dataclass(frozen=True)
class ProposalFaceEvidence:
    object_id:UUID
    reason:str|None
    threshold:float
    candidates:int
    overlapping_candidates:int

@dataclass(frozen=True)
class ProposalFiles:
    labels:Path
    protection:Path
    png:Path
    reservation:Reservation
    candidates:tuple[ProposedDefect,...]
    faces:tuple[ProposalFaceEvidence,...]


@contextmanager
def prepare_proposals(rgb:np.ndarray,image:WorkingImage,faces:Sequence[FaceInput],scratch:ScratchSpace,*,
                      permanent:np.ndarray|None=None,protections:Sequence[np.ndarray]=(),diameter_fraction:tuple[float,float]=(.002,.012),
                      max_roi_pixels:int=4194304,check_active:Callable[[],None]=lambda:None)->Iterator[ProposalFiles]:
    check_active();h,w=image.canvas.height,image.canvas.width
    if rgb.shape!=(h,w,3) or rgb.dtype!=np.float32 or h*w>200000000 or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':
        raise ValueError('Invalid proposal source')
    if len(faces)>64 or len({f.face.object_id for f in faces})!=len(faces):raise ValueError('Invalid proposal faces')
    for f in faces:
        if f.face.canvas!=image.canvas or f.face.transform_chain!=image.transform_chain:raise ValueError('Face geometry mismatch')
        for mask in (f.skin_probability,f.manual):
            if mask is not None and (mask.shape!=(h,w) or mask.dtype!=np.float32):raise ValueError('Invalid face mask')

    protections=tuple(protections)
    if len(protections)>64:raise ValueError('Protection input limit')
    required=protections if permanent is None else (permanent,*protections)
    for mask in required:
        if mask.shape!=(h,w) or mask.dtype!=np.float32:raise ValueError('Invalid permanent mask')
    with scratch.reserve(h*w*6+16384+png16_bound(w,h)) as reservation:
        label_path=reservation.path('defect-labels','.npy');protection_path=reservation.path('defect-protection','.npy')
        png_path=reservation.path('defect-labels','.png')
        candidates=[];evidence=[]
        with ExitStack() as maps:
            labels=np.lib.format.open_memmap(label_path,mode='w+',dtype='<u2',shape=(h,w));maps.callback(labels._mmap.close)
            protection=np.lib.format.open_memmap(protection_path,mode='w+',dtype='<f4',shape=(h,w));maps.callback(protection._mmap.close)
            for y in range(0,h,256):
                for x in range(0,w,256):
                    check_active();sl=(slice(y,y+256),slice(x,x+256));block=rgb[sl]
                    if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):raise ValueError('Invalid source values')
                    labels[sl]=0
                    protection[sl]=0
                    for source in required:
                        check_active();block=source[sl]
                        if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):raise ValueError('Invalid permanent values')
                        np.maximum(protection[sl],block,out=protection[sl])
            plans=[]
            for entry in sorted(faces,key=lambda f:f.face.object_id.hex):
                check_active();plan=plan_proposal_face(entry,max_roi_pixels=max_roi_pixels,check_active=check_active)
                if plan is None:
                    evidence.append(ProposalFaceEvidence(entry.face.object_id,'degenerate_landmarks',0.,0,0))
                    continue
                r=plan.rect
                sl=(slice(r.y,r.y+r.height),slice(r.x,r.x+r.width))
                parsing=None if entry.skin_probability is None else entry.skin_probability[sl]
                manual=None if entry.manual is None else entry.manual[sl]
                mask=build_skin_mask(entry.face,r,parsing,manual=manual,manual_mode=entry.manual_mode,max_work_pixels=max_roi_pixels,check_active=check_active)
                np.maximum(protection[sl],mask.protected,out=protection[sl])
                if mask.reason:evidence.append(ProposalFaceEvidence(entry.face.object_id,mask.reason,0.,0,0))
                else:plans.append((entry,plan,sl))
                del mask
            for entry,plan,sl in plans:
                check_active();r=plan.rect
                parsing=None if entry.skin_probability is None else entry.skin_probability[sl]
                manual=None if entry.manual is None else entry.manual[sl]
                mask=build_skin_mask(entry.face,r,parsing,manual=manual,permanent=protection[sl],manual_mode=entry.manual_mode,
                    max_work_pixels=max_roi_pixels,check_active=check_active)
                proposed=propose_defects(rgb[sl],mask.skin,protection[sl],plan.face_width,confidence=plan.confidence,
                    diameter_fraction=diameter_fraction,max_work_pixels=max_roi_pixels,check_active=check_active)
                added=overlapping=0
                for c in proposed.candidates:
                    check_active();b=c.bbox;local=(slice(b.y,b.y+b.height),slice(b.x,b.x+b.width))
                    full=(slice(r.y+b.y,r.y+b.y+b.height),slice(r.x+b.x,r.x+b.x+b.width))
                    selected=proposed.labels[local]==c.label
                    if np.any(labels[full][selected]!=0):overlapping+=1;continue
                    if len(candidates)>=4096:raise ValueError('DEFECT_CANDIDATE_LIMIT')
                    label=len(candidates)+1;labels[full][selected]=label
                    candidates.append(ProposedDefect(label,Rect(x=r.x+b.x,y=r.y+b.y,width=b.width,height=b.height),c.diameter,c.score));added+=1
                evidence.append(ProposalFaceEvidence(entry.face.object_id,proposed.reason,proposed.threshold,added,overlapping))
                del mask,proposed
            labels.flush();protection.flush();reservation.validate()
            with scratch.open_writer(png_path,reservation) as stream:
                write_png16(labels,stream,max_bytes=png16_bound(w,h),check_active=check_active)
            reservation.validate()
        check_active()
        yield ProposalFiles(label_path,protection_path,png_path,reservation,tuple(candidates),tuple(sorted(evidence,key=lambda f:f.object_id.hex)))

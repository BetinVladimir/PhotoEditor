"""Scratch-backed multi-face beauty against an immutable working image."""
from contextlib import contextmanager,ExitStack
from dataclasses import dataclass
from collections.abc import Callable,Iterator,Sequence
from pathlib import Path
from uuid import UUID
import numpy as np
from photo_worker.contracts.images import Face68,WorkingImage
from photo_worker.contracts.style import Beauty
from photo_worker.algorithms.beauty_roi import plan_roi
from photo_worker.algorithms.skin_mask import build_skin_mask
from photo_worker.algorithms.beauty import beauty_filter
from photo_worker.algorithms.masks import compose
from photo_worker.algorithms.face_composite import FaceCorrection,composite_faces
from photo_worker.runtime.scratch import ScratchSpace,Reservation

@dataclass(frozen=True)
class FaceInput:
    face:Face68
    skin_probability:np.ndarray|None
    manual:np.ndarray|None=None
    manual_mode:bool=False

@dataclass(frozen=True)
class FaceEvidence:
    object_id:UUID
    applied:bool
    reason:str|None
    texture_energy_ratio:float|None=None
    below_zero:float=0.
    above_one:float=0.

@dataclass(frozen=True)
class BeautyFiles:
    image:Path
    reservation:Reservation
    faces:tuple[FaceEvidence,...]


@contextmanager
def prepare_beauty(rgb:np.ndarray,image:WorkingImage,faces:Sequence[FaceInput],params:Beauty,scratch:ScratchSpace,*,
                   permanent:np.ndarray|None=None,tile:int=512,max_roi_pixels:int=4194304,
                   check_active:Callable[[],None]=lambda:None)->Iterator[BeautyFiles]:
    check_active();h,w=image.canvas.height,image.canvas.width
    if rgb.shape!=(h,w,3) or rgb.dtype!=np.float32 or h*w>200000000:raise ValueError('Invalid beauty source')
    if len(faces)>64 or len({f.face.object_id for f in faces})!=len(faces):raise ValueError('Duplicate/too many faces')
    if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('Invalid beauty color')
    for entry in faces:
        if entry.face.canvas!=image.canvas or entry.face.transform_chain!=image.transform_chain:raise ValueError('Face geometry mismatch')
        for mask in (entry.skin_probability,entry.manual):
            if mask is not None and (mask.shape!=(h,w) or mask.dtype!=np.float32):raise ValueError('Face mask layout mismatch')
    if permanent is not None and (permanent.shape!=(h,w) or permanent.dtype!=np.float32):raise ValueError('Permanent mask layout mismatch')
    with scratch.reserve(h*w*12+8192) as output_reservation:
        output_path=output_reservation.path('beauty-rgb','.npy')
        with ExitStack() as temp:
            def temporary(shape,role):
                pixels=int(np.prod(shape))
                reservation=temp.enter_context(scratch.reserve(pixels*4+8192))
                path=reservation.path(role,'.npy')
                array=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=shape)
                temp.callback(array._mmap.close)
                return array,reservation
            protection,protection_res=temporary((h,w),'global-protection')
            for y in range(0,h,256):
                for x in range(0,w,256):
                    check_active();sl=(slice(y,y+256),slice(x,x+256))
                    if permanent is None:protection[sl]=0
                    else:
                        part=permanent[sl]
                        if not np.isfinite(part).all() or np.any(part<0) or np.any(part>1):raise ValueError('Invalid permanent mask')
                        protection[sl]=part
            plans=[];evidence=[]
            for entry in sorted(faces,key=lambda f:f.face.object_id.hex):
                check_active();plan=plan_roi(entry.face,max_work_pixels=max_roi_pixels);r=plan.rect
                sl=(slice(r.y,r.y+r.height),slice(r.x,r.x+r.width))
                parsing=None if entry.skin_probability is None else entry.skin_probability[sl]
                manual=None if entry.manual is None else entry.manual[sl]
                mask=build_skin_mask(entry.face,r,parsing,manual=manual,manual_mode=entry.manual_mode,
                                     max_work_pixels=max_roi_pixels,check_active=check_active)
                np.maximum(protection[sl],mask.protected,out=protection[sl])
                if mask.reason:evidence.append(FaceEvidence(entry.face.object_id,False,mask.reason))
                else:plans.append((entry,plan,sl))
                del mask
            protection.flush();protection_res.validate()
            corrections=[]
            for entry,plan,sl in plans:
                check_active();r=plan.rect
                parsing=None if entry.skin_probability is None else entry.skin_probability[sl]
                manual=None if entry.manual is None else entry.manual[sl]
                mask=build_skin_mask(entry.face,r,parsing,manual=manual,permanent=protection[sl],manual_mode=entry.manual_mode,
                                     max_work_pixels=max_roi_pixels,check_active=check_active)
                result=beauty_filter(rgb[sl],mask.skin,protection[sl],plan.face_width,params,confidence=plan.confidence,
                                     automatic=not entry.manual_mode,max_work_pixels=max_roi_pixels,check_active=check_active)
                evidence.append(FaceEvidence(entry.face.object_id,result.applied,result.reason,result.texture_energy_ratio,result.below_zero,result.above_one))
                if result.applied:
                    delta,res=temporary((r.height,r.width,3),'face-delta');delta[:]=result.rgb-rgb[sl];delta.flush();res.validate()
                    weight,res=temporary((r.height,r.width),'face-weight')
                    compose(mask.skin,protected=protection[sl],out=weight,check_active=check_active);weight.flush();res.validate()
                    delta.flags.writeable=False;weight.flags.writeable=False
                    corrections.append(FaceCorrection(entry.face.object_id,r,delta,weight))
                del mask,result
            output=np.lib.format.open_memmap(output_path,mode='w+',dtype='<f4',shape=(h,w,3));temp.callback(output._mmap.close)
            for part in composite_faces(rgb,corrections,tile_size=tile,check_active=check_active):
                th,tw=part.rgb.shape[:2];output[part.y:part.y+th,part.x:part.x+tw]=part.rgb
            output.flush();output_reservation.validate()
        check_active()
        yield BeautyFiles(output_path,output_reservation,tuple(sorted(evidence,key=lambda e:e.object_id.hex)))

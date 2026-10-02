"""DoG candidate generation; heuristic scores never constitute approval."""
from dataclasses import dataclass
from collections.abc import Callable
import math
import cv2
import numpy as np
from photo_worker.contracts.images import Rect
from .color import LUMA

@dataclass(frozen=True)
class ProposedDefect:
    label:int
    bbox:Rect
    diameter:float
    score:float

@dataclass(frozen=True)
class ProposalResult:
    labels:np.ndarray
    candidates:tuple[ProposedDefect,...]
    threshold:float
    reason:str|None
    algorithm:str='defect_dog_v1'


def propose_defects(rgb:np.ndarray,skin:np.ndarray,protected:np.ndarray,face_width:float,*,confidence:float=1.,
                    diameter_fraction:tuple[float,float]=(.002,.012),max_work_pixels:int=4194304,
                    check_active:Callable[[],None]=lambda:None)->ProposalResult:
    check_active()
    if type(max_work_pixels) is not int or not 1<=max_work_pixels<=4194304:raise ValueError('Invalid proposal budget')
    if rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[2]!=3 or min(rgb.shape[:2])<1 or rgb.shape[0]*rgb.shape[1]>max_work_pixels:
        raise ValueError('Invalid proposal ROI')
    h,w=rgb.shape[:2]
    if skin.shape!=(h,w) or protected.shape!=(h,w) or skin.dtype!=np.float32 or protected.dtype!=np.float32:
        raise ValueError('Invalid proposal masks')
    if not math.isfinite(face_width) or not 0<face_width<=max(h,w) or not math.isfinite(confidence) or not 0<=confidence<=1:
        raise ValueError('Invalid face scale/confidence')
    lo,hi=diameter_fraction
    if not .002<=lo<=hi<=.012:raise ValueError('Invalid defect diameter fractions')
    for array in (rgb,skin,protected):
        if not np.isfinite(array).all() or np.any(array<0) or np.any(array>1):raise ValueError('Invalid proposal values')
    labels=np.zeros((h,w),np.uint16)
    def result(candidates=(),threshold=0.,reason=None):
        labels.flags.writeable=False
        return ProposalResult(labels,tuple(candidates),threshold,reason)
    if confidence<.75:return result(reason='low_confidence')
    if face_width<64:return result(reason='face_too_small')
    eligible=(skin>=.75)&(protected==0)
    if not np.any(eligible):return result(reason='empty_skin')
    minimum=max(1.,lo*face_width);maximum=hi*face_width
    if maximum<minimum:return result(reason='subpixel_diameter')
    luma=np.sum(rgb*LUMA.astype(np.float32),axis=-1,dtype=np.float32)
    redness=rgb[...,0]-(rgb[...,1]+rgb[...,2])*.5
    response=np.zeros((h,w),np.float32)
    def blur(signal,sigma):
        r=math.ceil(3*sigma)
        return cv2.GaussianBlur(signal,(2*r+1,2*r+1),sigmaX=sigma,sigmaY=sigma,borderType=cv2.BORDER_REFLECT_101)
    for diameter in np.geomspace(minimum,maximum,5):
        check_active();sigma=max(.5,float(diameter)/(2*math.sqrt(2)))
        dark=blur(luma,sigma*1.6)-blur(luma,sigma)
        check_active();red=blur(redness,sigma)-blur(redness,sigma*1.6)
        np.maximum(response,np.maximum(dark,red),out=response)
    check_active();sample=response[eligible]
    median=float(np.median(sample));mad=float(np.median(np.abs(sample-median)))
    threshold=max(.015,median+4*1.4826*mad)
    binary=((response>threshold)&eligible).astype(np.uint8)
    check_active();count,components,stats,_=cv2.connectedComponentsWithStats(binary,connectivity=8)
    if count>65536:raise ValueError('DEFECT_COMPONENT_LIMIT')
    # The low response threshold discovers blobs; their half-peak core defines
    # the repair mask. Otherwise the DoG halo inflates a3px defect to5px.
    cores=np.zeros_like(binary)
    for component in range(1,count):
        check_active();x,y,cw,ch,_=map(int,stats[component]);region=(slice(y,y+ch),slice(x,x+cw))
        selected=components[region]==component
        peak=float(np.max(response[region][selected]))
        cores[region][selected & (response[region]>=max(threshold,.5*peak))]=1
    check_active();count,components,stats,_=cv2.connectedComponentsWithStats(cores,connectivity=8)
    if count>65536:raise ValueError('DEFECT_COMPONENT_LIMIT')
    candidates=[]
    # Components are explicitly sorted by bbox; IDs do not depend on thread order.
    order=sorted(range(1,count),key=lambda i:(int(stats[i,1]),int(stats[i,0]),i))
    for component in order:
        check_active();x,y,cw,ch,area=map(int,stats[component]);diameter=math.sqrt(4*area/math.pi)
        if not minimum<=diameter<=maximum or area/(cw*ch)<.35 or x==0 or y==0 or x+cw==w or y+ch==h:continue
        if len(candidates)>=4096:raise ValueError('DEFECT_CANDIDATE_LIMIT')
        region=(slice(y,y+ch),slice(x,x+cw));selected=components[region]==component
        peak=float(np.max(response[region][selected]));score=max(0.,min(1.,1-threshold/peak))
        label=len(candidates)+1;labels[region][selected]=label
        candidates.append(ProposedDefect(label,Rect(x=x,y=y,width=cw,height=ch),diameter,score))
    check_active()
    return result(candidates,threshold)

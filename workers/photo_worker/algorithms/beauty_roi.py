"""Full-support face ROI planning, without downscaling or truncated halos."""
from dataclasses import dataclass
import math
from photo_worker.contracts.images import Face68,Rect

@dataclass(frozen=True)
class BeautySupport:
    radius:int
    sigma_low:float
    sigma_fine:float
    halo:int


def filter_support(face_width:float)->BeautySupport:
    if not math.isfinite(face_width) or face_width<=0:raise ValueError('Invalid face width')
    radius=max(2,round(.025*face_width))
    low=max(1.,.04*face_width);fine=max(.6,.004*face_width)
    return BeautySupport(radius,low,fine,max(2*radius+math.ceil(3*low),math.ceil(3*fine)))

@dataclass(frozen=True)
class BeautyROI:
    rect:Rect
    face_width:float
    confidence:float
    support:BeautySupport


def plan_roi(face:Face68,*,max_work_pixels:int=4194304)->BeautyROI:
    """Clamp only at actual frame boundaries; never reduce filter radius to fit."""
    if type(max_work_pixels) is not int or not 1<=max_work_pixels<=4194304:
        raise ValueError('Invalid beauty ROI budget')
    face.chain_matches()
    xs=[p.x*face.canvas.width-.5 for p in face.points]
    ys=[p.y*face.canvas.height-.5 for p in face.points]
    width=max(xs[:17])-min(xs[:17]);height=max(ys)-min(ys)
    if width<=0 or height<=0 or max(width,height)>4*min(width,height):
        raise ValueError('DEGENERATE_FACE_LANDMARKS')
    support=filter_support(width)
    # Extra pixel handles integer rounding of conservative feature hulls.
    halo=support.halo+1
    x0=max(0,math.floor(min(xs))-halo);y0=max(0,math.floor(min(ys))-halo)
    x1=min(face.canvas.width,math.ceil(max(xs))+halo+1)
    y1=min(face.canvas.height,math.ceil(max(ys))+halo+1)
    if (x1-x0)*(y1-y0)>max_work_pixels:raise ValueError('BEAUTY_ROI_BUDGET_EXCEEDED')
    return BeautyROI(Rect(x=x0,y=y0,width=x1-x0,height=y1-y0),width,min(p.confidence for p in face.points),support)

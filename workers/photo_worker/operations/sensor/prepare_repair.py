"""Shared owned sensor-space dust repair with unchanged Telea processing."""
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path
import cv2
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.dust import DustCandidate,DustRepairEvidence,SensorCrop


@dataclass(frozen=True)
class PreparedDustRepair:
    path: Path
    reservation: object
    candidates: tuple[DustRepairEvidence,...]


def dust_repair_ram_bytes(width,height):
    """Conservative full-frame native buffers; qualify measured peaks separately."""
    if (type(width) is not int or type(height) is not int or min(width,height)<1
            or width*height>200000000):
        raise ValueError('DUST_REPAIR_INPUT_INVALID')
    return max(536870912,128*width*height+268435456)


def dust_circle_pixel_count(width,height,x,y,radius,*,check_active=lambda:None):
    """Count the original rounded integer circle using bounded temporary tiles."""
    cx,cy,r=round(x),round(y),round(radius)
    left,right=max(0,cx-r),min(width,cx+r+1)
    top,bottom=max(0,cy-r),min(height,cy+r+1)
    count=0
    check_active()
    for row in range(top,bottom,256):
        yy=np.arange(row,min(row+256,bottom),dtype=np.int64)[:,None]-cy
        for col in range(left,right,256):
            check_active()
            xx=np.arange(col,min(col+256,right),dtype=np.int64)[None,:]-cx
            count+=int(np.count_nonzero(xx*xx+yy*yy<=r*r))
    return count


@contextmanager
def prepare_dust_repair(rgb,crop,selected,scratch,*,check_active=lambda:None):
    """Caller authorizes selection and admits full image/native RAM costs."""
    if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3
            or rgb.shape[2]!=3 or min(rgb.shape[:2])<1 or rgb.shape[0]*rgb.shape[1]>200000000
            or not isinstance(selected,(tuple,list)) or not 1<=len(selected)<=4096):
        raise ValueError('DUST_REPAIR_INPUT_INVALID')
    crop=SensorCrop.model_validate_json(canonical_bytes(crop))
    selected=tuple(DustCandidate.model_validate_json(canonical_bytes(item)) for item in selected)
    if len({item.id for item in selected})!=len(selected):raise ValueError('DUST_REPAIR_INPUT_INVALID')
    h,w=rgb.shape[:2]
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active();part=rgb[y:y+256,x:x+256]
            if not np.isfinite(part).all() or np.any(part<0) or np.any(part>1):
                raise ValueError('DUST_REPAIR_INPUT_INVALID')
    mask=np.zeros((h,w),np.uint8);mapped=[]
    for candidate in selected:
        check_active()
        x=(candidate.sensor_x-crop.x+.5)*w/crop.width-.5
        y=(candidate.sensor_y-crop.y+.5)*h/crop.height-.5
        radius=max(1.,candidate.radius_sensor_px*.5*(w/crop.width+h/crop.height))
        if x+radius<0 or y+radius<0 or x-radius>=w or y-radius>=h:
            raise ValueError('DUST_REPAIR_INPUT_INVALID')
        cv2.circle(mask,(round(x),round(y)),max(1,round(radius)),255,-1,lineType=cv2.LINE_8)
        mapped.append((candidate,x,y,radius))
    repaired=np.empty_like(rgb)
    inpaint_radius=min(32.,max(item[3] for item in mapped)+2.)
    for channel in range(3):
        check_active()
        repaired[:,:,channel]=cv2.inpaint(rgb[:,:,channel],mask,inpaint_radius,cv2.INPAINT_TELEA)
    np.clip(repaired,0,1,out=repaired)
    evidence=[]
    for candidate,x,y,radius in mapped:
        check_active()
        applied=dust_circle_pixel_count(w,h,x,y,radius,check_active=check_active)
        evidence.append(DustRepairEvidence(candidate_id=candidate.id,center_x=x,center_y=y,
            radius=radius,applied_pixels=applied))
    with scratch.reserve(h*w*12+8192) as reservation:
        path=reservation.path('dust-repaired-rgb','.npy')
        with scratch.open_writer(path,reservation) as output:
            np.save(output,repaired,allow_pickle=False)
        del repaired,mask
        check_active()
        yield PreparedDustRepair(path,reservation,tuple(evidence))
        check_active()

"""Float32 Telea for already approved masks; proposal/review binding is external."""
from dataclasses import dataclass
from collections.abc import Callable
import math
import cv2
import numpy as np

@dataclass(frozen=True)
class RepairResult:
    rgb:np.ndarray
    applied_pixels:int
    radius:float
    below_zero:float
    above_one:float


def repair_accepted(rgb:np.ndarray,accepted:np.ndarray,protected:np.ndarray,diameter:float,*,strength:float=1.,
                    max_work_pixels:int=4194304,check_active:Callable[[],None]=lambda:None)->RepairResult:
    """One supported ROI. No pixels outside accepted/unprotected mask may change.

    Telea operates on float32 scaled to16-bit units to avoid its fixed-unit
    numeric bias on normalized RGB. This is scaling, never integer quantization.
    Task adapter must verify saved approval IDs/source/mask hashes beforehand.
    """
    check_active()
    if type(max_work_pixels) is not int or not 1<=max_work_pixels<=4194304:raise ValueError('Invalid repair budget')
    if rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[2]!=3 or min(rgb.shape[:2])<1 or rgb.shape[0]*rgb.shape[1]>max_work_pixels:
        raise ValueError('Invalid repair ROI')
    if accepted.shape!=rgb.shape[:2] or accepted.dtype!=np.bool_ or protected.shape!=accepted.shape or protected.dtype!=np.float32:
        raise ValueError('Invalid repair masks')
    if not math.isfinite(diameter) or diameter<=0 or not math.isfinite(strength) or not 0<=strength<=1:raise ValueError('Invalid repair parameters')
    for array in (rgb,protected):
        if not np.isfinite(array).all() or np.any(array<0) or np.any(array>1):raise ValueError('Invalid repair values')
    allowed=accepted & (protected==0)
    radius=min(5.,max(1.,.25*diameter))
    if not np.any(allowed) or strength==0:
        result=rgb.copy();result.flags.writeable=False
        return RepairResult(result,0,radius,0.,0.)
    if np.any(allowed[0]) or np.any(allowed[-1]) or np.any(allowed[:,0]) or np.any(allowed[:,-1]):
        raise ValueError('INSUFFICIENT_REPAIR_CONTEXT')
    mask=allowed.astype(np.uint8)*255
    repaired=np.empty_like(rgb)
    for channel in range(3):
        check_active()
        samples=np.ascontiguousarray(rgb[...,channel]*np.float32(65535))
        repaired[...,channel]=cv2.inpaint(samples,mask,radius,cv2.INPAINT_TELEA)/np.float32(65535)
    check_active()
    below=float(np.mean(repaired[allowed]<0));above=float(np.mean(repaired[allowed]>1))
    distance=cv2.distanceTransform(allowed.astype(np.uint8),cv2.DIST_L2,cv2.DIST_MASK_PRECISE)
    alpha=np.minimum(distance/max(1.,radius),1.)*np.float32(strength)
    result=rgb+(np.clip(repaired,0,1)-rgb)*alpha[...,None]
    result[~allowed]=rgb[~allowed]
    result.flags.writeable=False;check_active()
    return RepairResult(result,int(np.count_nonzero(allowed)),radius,below,above)

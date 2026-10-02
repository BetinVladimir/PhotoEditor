"""BeautyFilterV1 on one complete supported face ROI, never independent tiles."""
from dataclasses import dataclass
from collections.abc import Callable
import math
import cv2
import numpy as np
from photo_worker.contracts.style import Beauty
from .color import LUMA
from .tiles import guided_filter
from .masks import compose
from .beauty_roi import filter_support

@dataclass(frozen=True)
class BeautyResult:
    rgb:np.ndarray
    applied:bool
    reason:str|None
    texture_energy_ratio:float|None
    below_zero:float
    above_one:float


def _gaussian(rgb,sigma):
    # Explicit support fixes the kernel across platforms and ROI implementations.
    half=max(1,math.ceil(3*sigma))
    return cv2.GaussianBlur(rgb,(2*half+1,2*half+1),sigmaX=sigma,sigmaY=sigma,borderType=cv2.BORDER_REFLECT_101)


def beauty_filter(rgb:np.ndarray,skin:np.ndarray,protected:np.ndarray,face_width:float,params:Beauty,*,
                  confidence:float=1.,automatic:bool=True,max_work_pixels:int=4194304,
                  check_active:Callable[[],None]=lambda:None)->BeautyResult:
    """Caller provides full face ROI plus filter support and effective params.

    Median is per-face/per-channel over trusted skin. No cross-person tone target.
    Texture_keep is a preservation constraint and never scaled by style strength.
    """
    check_active()
    if type(max_work_pixels) is not int or max_work_pixels<1 or max_work_pixels>4194304:
        raise ValueError('Invalid beauty working budget')
    if rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[2]!=3 or min(rgb.shape[:2])<1 or rgb.shape[0]*rgb.shape[1]>max_work_pixels:
        raise ValueError('Invalid beauty ROI')
    if not math.isfinite(face_width) or not 0<face_width<=max(rgb.shape[:2]) or not math.isfinite(confidence) or not 0<=confidence<=1:
        raise ValueError('Invalid face scale/confidence')
    for array in (rgb,skin,protected):
        if not np.isfinite(array).all() or np.any(array<0) or np.any(array>1):raise ValueError('Invalid beauty values')
    if skin.shape!=rgb.shape[:2] or protected.shape!=skin.shape or skin.dtype!=np.float32 or protected.dtype!=np.float32:
        raise ValueError('Invalid beauty masks')
    def skipped(reason):
        output=rgb.copy();output.flags.writeable=False
        return BeautyResult(output,False,reason,None,0.,0.)
    if automatic and confidence<.75:return skipped('low_confidence')
    if automatic and face_width<64:return skipped('face_too_small')
    if params.smoothing==0 and params.tone_evenness==0:return skipped('zero_strength')
    mask=compose(skin,protected=protected,check_active=check_active)
    trusted=(skin>=.5)&(protected<.5)
    if not np.any(trusted) or not np.any(mask):return skipped('empty_skin')
    support=filter_support(face_width)
    r,sigma_low,sigma_fine=support.radius,support.sigma_low,support.sigma_fine
    check_active()
    luma=np.sum(rgb*LUMA.astype(np.float32),axis=-1,dtype=np.float32)
    base=guided_filter(luma,rgb,r,.002)
    check_active();low=_gaussian(base,sigma_low)
    check_active();fine=rgb-_gaussian(rgb,sigma_fine)
    median=np.median(low[trusted],axis=0)
    smoothing_delta=base+params.texture_keep*fine-rgb
    tone_delta=np.clip(median-low,-.03,.03)
    output=rgb+mask[...,None]*(params.smoothing*smoothing_delta+params.tone_evenness*tone_delta)
    below=float(np.mean(output<0));above=float(np.mean(output>1))
    output=np.clip(output,0,1).astype(np.float32)
    output[mask==0]=rgb[mask==0]
    check_active();result_fine=output-_gaussian(output,sigma_fine)
    weights=mask.astype(np.float64)[...,None]
    before=float(np.sum(fine.astype(np.float64)**2*weights))
    after=float(np.sum(result_fine.astype(np.float64)**2*weights))
    ratio=after/before if before>1e-12 else None
    check_active();output.flags.writeable=False
    return BeautyResult(output,True,None,ratio,below,above)

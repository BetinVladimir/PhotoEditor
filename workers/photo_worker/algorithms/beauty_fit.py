import itertools
import cv2
import numpy as np
from photo_worker.algorithms.beauty import beauty_filter
from photo_worker.contracts.style import Beauty

GRID=(0.,.1,.25,.5,.75,1.)
TEXTURE=(.75,.9,1.)


def _loss(candidate,target,mask):
    trusted=mask>=.5
    if not np.any(trusted):return float('inf')
    low_c=cv2.GaussianBlur(candidate,(0,0),2);low_t=cv2.GaussianBlur(target,(0,0),2)
    mid_c=candidate-cv2.GaussianBlur(candidate,(0,0),.8);mid_t=target-cv2.GaussianBlur(target,(0,0),.8)
    residual=float(np.mean((low_c[trusted]-low_t[trusted])**2)+.5*np.mean((mid_c[trusted]-mid_t[trusted])**2))
    src_energy=float(np.mean(mid_t[trusted]**2));out_energy=float(np.mean(mid_c[trusted]**2))
    return residual+max(0.,src_energy-out_energy)*.25


def fit_face(source,target,skin,protected,face_width,confidence,check_active=lambda:None):
    baseline=_loss(source,target,skin);best=(baseline,(0.,0.,1.),None)
    for smoothing,tone,texture in itertools.product(GRID,GRID,TEXTURE):
        check_active();params=Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=smoothing,tone_evenness=tone,texture_keep=texture,skin_mask='face68_conservative_v1',protect_features=True)
        result=beauty_filter(source,skin,protected,face_width,params,confidence=confidence,max_work_pixels=4194304,check_active=check_active)
        loss=_loss(result.rgb,target,skin)
        if (loss,smoothing,tone,texture)<(best[0],*best[1]):best=(loss,(smoothing,tone,texture),result.texture_energy_ratio)
    # Three bounded coordinate refinements around the best discrete solution.
    values=list(best[1])
    for _ in range(3):
        for axis in range(3):
            for delta in (-.05,.05):
                trial=values.copy();trial[axis]=float(np.clip(trial[axis]+delta,.75 if axis==2 else 0,1))
                params=Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=trial[0],tone_evenness=trial[1],texture_keep=trial[2],skin_mask='face68_conservative_v1',protect_features=True)
                result=beauty_filter(source,skin,protected,face_width,params,confidence=confidence,max_work_pixels=4194304,check_active=check_active);loss=_loss(result.rgb,target,skin)
                if loss<best[0]:best=(loss,tuple(trial),result.texture_energy_ratio);values=trial
    return baseline,best

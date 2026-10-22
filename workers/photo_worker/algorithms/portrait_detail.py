import cv2
import numpy as np
from .color import to_linear,to_encoded,LUMA


def apply_region(rgb,mask,kind,strength,protected,check_active=lambda:None):
    if rgb.dtype!=np.float32 or mask.shape!=rgb.shape[:2] or protected.shape!=mask.shape:raise ValueError('Invalid portrait detail arrays')
    weight=np.clip(mask,0,1)*(1-np.clip(protected,0,1))
    # Protected probabilities are a hard exclusion, including feathered edges.
    # A fractional guard must never become permission for a smaller edit.
    weight=np.where(protected>0,0,weight)
    changed=int(np.count_nonzero(weight>1/65535))
    if strength==0:return rgb.copy(),0,0.,0.,'zero_strength'
    if changed==0:return rgb.copy(),0,0.,0.,'empty_mask'
    if kind=='glasses':return rgb.copy(),0,0.,0.,'review_required'
    check_active();linear=to_linear(rgb);luma=np.sum(linear*LUMA.astype(np.float32),axis=-1);low=cv2.GaussianBlur(luma,(0,0),max(1,min(rgb.shape[:2])*.015));delta=np.zeros_like(linear);max_ev=.0;chroma=.0
    if kind in ('teeth','sclera'):
        max_ev=.2*strength;gain=2**max_ev;gray=np.sum(linear*LUMA.astype(np.float32),axis=-1,keepdims=True);chroma=.25*strength;target=(linear*(1-chroma)+gray*chroma)*gain;delta=target-linear
    elif kind=='shine':
        max_ev=.3*strength;target=np.maximum(0,luma*(2**(-max_ev)));delta=(target-luma)[...,None]
    elif kind in ('under_eye','cloth'):
        max_ev=.3*strength;median=float(np.median(low[weight>.5])) if np.any(weight>.5) else float(np.median(low));target=np.clip(median-low,-(.3*strength),.3*strength);delta=linear*np.expm1(np.log(2)*target[...,None])
    out=to_encoded(np.clip(linear+weight[...,None]*delta,0,1)).astype(np.float32);out[weight==0]=rgb[weight==0];return out,changed,max_ev,chroma,None

"""Bounded ordered adjustment-layer blending with exact protected bypass."""
from dataclasses import dataclass
import math
import numpy as np
from photo_worker.contracts.images import ColorSpace
from photo_worker.operations.optical.color import to_working_linear,from_working_linear

@dataclass(frozen=True)
class ComposedTile:
    rgb:np.ndarray
    alpha:np.ndarray|None
    changed:np.ndarray


def compose_layer_tile(source,proposal,mask,*,color:ColorSpace,opacity:float,blend:str,
                       protected=None,source_alpha=None,proposal_alpha=None,
                       max_tile_bytes:int,check_active=lambda:None)->ComposedTile:
    """Opacity acts once on the mask, then S+E*(B(S,P)-S) in linear RGB.

    Alpha follows the same adjustment interpolation, not repeated source-over
    accumulation. Inputs are float32 straight RGB/alpha tiles. Legacy uint16
    alpha requires an explicit adapter; it is never silently quantized here.
    """
    check_active()
    if (not isinstance(source,np.ndarray) or not isinstance(proposal,np.ndarray) or
        source.dtype!=np.float32 or proposal.dtype!=np.float32 or source.shape!=proposal.shape or
        source.ndim!=3 or source.shape[-1]!=3 or min(source.shape[:2])<1 or max(source.shape[:2])>1024 or
        type(opacity) not in (float,int) or not math.isfinite(opacity) or not 0<=opacity<=1 or
        blend not in ('normal','multiply','screen') or type(max_tile_bytes) is not int or
        source.shape[0]*source.shape[1]*256>max_tile_bytes):raise ValueError('LAYER_TILE_INVALID')
    shape=source.shape[:2]
    for value in (source,proposal):
        if not np.isfinite(value).all():raise ValueError('LAYER_NONFINITE')
    for value in (mask,protected,source_alpha,proposal_alpha):
        if value is not None and (not isinstance(value,np.ndarray) or value.shape!=shape or value.dtype!=np.float32 or not np.isfinite(value).all()):raise ValueError('LAYER_MASK_ALPHA_INVALID')
    if mask is None:raise ValueError('LAYER_MASK_ALPHA_INVALID')
    for value in (source_alpha,proposal_alpha):
        if value is not None and (np.any(value<0) or np.any(value>1)):raise ValueError('LAYER_ALPHA_INVALID')
    weight=np.clip(mask,0,1).astype(np.float64)*opacity
    if protected is not None:
        weight*=1-np.clip(protected,0,1)
        weight[protected>0]=0
    active=weight>0
    output=source.copy()
    alpha=None if source_alpha is None else source_alpha.copy()
    changed=np.zeros(shape,np.float32)
    if not np.any(active):
        check_active();return ComposedTile(output,alpha,changed)
    check_active()
    if alpha is None and proposal_alpha is not None:alpha=np.ones(shape,np.float32)
    full=active & (weight==1) if blend=='normal' else np.zeros(shape,bool)
    if blend=='normal':output[full]=proposal[full]
    same=np.all(source.view(np.uint32)==proposal.view(np.uint32),axis=-1) if blend=='normal' else np.zeros(shape,bool)
    processing=active & ~full & ~same
    if np.any(processing):
        # Only modified samples enter transfer math: protected/exact endpoints
        # must not suffer an unrelated transfer round-trip or HDR overflow.
        s=to_working_linear(source[processing][None,...],color).astype(np.float64)
        p=to_working_linear(proposal[processing][None,...],color).astype(np.float64)
        with np.errstate(over='ignore',invalid='ignore'):
            if blend=='normal':candidate=p
            elif blend=='multiply':candidate=s*p
            else:candidate=1-(1-s)*(1-p)
            linear=(s+(candidate-s)*weight[processing][None,:,None]).astype(np.float32)
        if not np.isfinite(linear).all():raise ValueError('LAYER_COLOR_OVERFLOW')
        encoded=from_working_linear(linear,color)[0]
        unchanged=linear[0].view(np.uint32)==s[0].astype(np.float32).view(np.uint32)
        original=source[processing]
        encoded[unchanged]=original[unchanged]
        output[processing]=encoded
    if alpha is not None:
        original=np.ones(shape,np.float32) if source_alpha is None else source_alpha
        candidate=np.ones(shape,np.float32) if proposal_alpha is None else proposal_alpha
        blended=(original.astype(np.float64)+(candidate.astype(np.float64)-original)*weight).astype(np.float32)
        alpha[active]=blended[active]
        alpha[active & (weight==1)]=candidate[active & (weight==1)]
        unchanged_alpha=original.view(np.uint32)==candidate.view(np.uint32)
        alpha[unchanged_alpha]=original[unchanged_alpha]
        changed[np.any(output.view(np.uint32)!=source.view(np.uint32),axis=-1)|(alpha.view(np.uint32)!=original.view(np.uint32))]=1
    else:changed[np.any(output.view(np.uint32)!=source.view(np.uint32),axis=-1)]=1
    check_active()
    return ComposedTile(output,alpha,changed)

"""V28 depth-gated exposure field; no normals or physical lighting inference.

Inputs must already have authorized, matching canvases and calibrated depth.
Processing uses bounded blocks; callers may supply a scratch-backed output.
"""
import math
import numpy as np
from photo_worker.contracts.relight import RelightLight


def _blocks(h,w):
    for y in range(0,h,256):
        for x in range(0,w,512):
            yield y,min(h,y+256),x,min(w,x+512)


def _anchor(depth,invalid,x,y,check_active):
    h,w=depth.shape
    px=np.clip(x,0,1)*(w-1);py=np.clip(y,0,1)*(h-1)
    x0,y0=int(math.floor(px)),int(math.floor(py))
    x1,y1=min(w-1,x0+1),min(h-1,y0+1)
    dx,dy=px-x0,py-y0
    weighted=((y0,x0,(1-dx)*(1-dy)),(y0,x1,dx*(1-dy)),(y1,x0,(1-dx)*dy),(y1,x1,dx*dy))
    if all(weight==0 or not invalid[row,col] for row,col,weight in weighted):
        return sum(float(depth[row,col])*weight for row,col,weight in weighted if weight>0)
    best=None
    for top,bottom,left,right in _blocks(h,w):
        check_active()
        rows,cols=np.nonzero(~invalid[top:bottom,left:right])
        if not rows.size:continue
        rows=rows+top;cols=cols+left
        distance=(rows-py)**2+(cols-px)**2
        index=int(np.argmin(distance))
        candidate=(float(distance[index]),int(rows[index]),int(cols[index]))
        if best is None or candidate<best:best=candidate
    if best is None:raise ValueError('DEPTH_UNAVAILABLE')
    return float(depth[best[1],best[2]])


def build_light_field(depth,invalid,lights,depth_sigma,*,out=None,check_active=lambda:None):
    """Return float32 EV field clamped to [-1,1], with invalid depth set to0.

    Coordinates are normalized across the full canvas (single-pixel axes use0).
    Spatial centers retain allowed off-canvas coordinates; only depth sampling
    is clamped. Nearest valid depth ties select (row,col) deterministically.
    """
    check_active()
    if (not isinstance(depth,np.ndarray) or depth.dtype!=np.float32 or depth.ndim!=2
            or min(depth.shape)<1 or depth.size>200000000
            or not isinstance(invalid,np.ndarray) or invalid.dtype!=np.bool_ or invalid.shape!=depth.shape
            or not isinstance(lights,(list,tuple)) or not 1<=len(lights)<=4
            or any(not isinstance(light,RelightLight) for light in lights)
            or not math.isfinite(depth_sigma) or not .01<=depth_sigma<=1):
        raise ValueError('RELIGHT_INPUT_INVALID')
    h,w=depth.shape;valid_count=0
    for y,y1,x,x1 in _blocks(h,w):
        check_active();valid=~invalid[y:y1,x:x1];values=depth[y:y1,x:x1][valid]
        if not np.isfinite(values).all() or np.any(values<0) or np.any(values>1):raise ValueError('RELIGHT_DEPTH_INVALID')
        valid_count+=int(valid.sum())
    if not valid_count:raise ValueError('DEPTH_UNAVAILABLE')
    if out is None:out=np.empty_like(depth)
    if not isinstance(out,np.ndarray) or out.shape!=depth.shape or out.dtype!=np.float32 or not out.flags.writeable or np.shares_memory(out,depth) or np.shares_memory(out,invalid):
        raise ValueError('RELIGHT_OUTPUT_INVALID')
    anchors=[_anchor(depth,invalid,light.x,light.y,check_active) for light in lights]
    for y,y1,x,x1 in _blocks(h,w):
        check_active()
        yy=np.arange(y,y1,dtype=np.float64)[:,None]/max(1,h-1)
        xx=np.arange(x,x1,dtype=np.float64)[None,:]/max(1,w-1)
        valid=~invalid[y:y1,x:x1]
        d=np.where(valid,depth[y:y1,x:x1],0).astype(np.float64)
        field=np.zeros(d.shape,np.float64)
        for light,d0 in zip(lights,anchors):
            spatial=((xx-light.x)**2+(yy-light.y)**2)/(2*light.radius**2)
            field+=light.ev*np.exp(-spatial)*np.exp(-(d-d0)**2/(2*depth_sigma**2))
        out[y:y1,x:x1]=np.where(valid,np.clip(field,-1,1),0).astype(np.float32)
    check_active()
    return out


def relight_proposal(source,field):
    """Scale linear RGB by the bounded EV field, preserving HDR range."""
    if (not isinstance(source,np.ndarray) or source.dtype!=np.float32 or source.ndim!=3 or source.shape[-1]!=3
            or not isinstance(field,np.ndarray) or field.dtype!=np.float32 or field.shape!=source.shape[:2]
            or not np.isfinite(source).all() or not np.isfinite(field).all() or np.any(np.abs(field)>1)):
        raise ValueError('RELIGHT_INPUT_INVALID')
    with np.errstate(over='ignore',invalid='ignore'):
        result=(source.astype(np.float64)*np.exp2(field.astype(np.float64))[...,None]).astype(np.float32)
    if not np.isfinite(result).all():raise ValueError('RELIGHT_OVERFLOW')
    return result

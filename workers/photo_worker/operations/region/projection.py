"""Coordinate projection through actual transforms; never resizes RGB back.

Caller authenticates the transform chain and dense field before invoking this
pure projection. Rectangles use pixel edges, matrices integer pixel centres.
"""
from dataclasses import dataclass
import math
import numpy as np
from photo_worker.contracts.images import CropTransform, ResizeTransform, MatrixTransform, DenseWarpTransform


@dataclass(frozen=True)
class ProjectedBounds:
    left: int
    top: int
    width: int
    height: int


def project_region_bounds(bounds,step,*,inverse_field=None,check_active=lambda:None):
    sw,sh=step.from_canvas.width,step.from_canvas.height
    w,h=step.to_canvas.width,step.to_canvas.height
    x0,y0=bounds.left,bounds.top;x1,y1=x0+bounds.width,y0+bounds.height
    if not (0<=x0<x1<=sw and 0<=y0<y1<=sh):raise ValueError('REGION_PROJECTION_SOURCE_INVALID')
    check_active()
    if isinstance(step,CropTransform):
        x0-=step.crop_rect.x;x1-=step.crop_rect.x;y0-=step.crop_rect.y;y1-=step.crop_rect.y
    elif isinstance(step,ResizeTransform):
        x0,x1=x0*w/sw,x1*w/sw;y0,y1=y0*h/sh,y1*h/sh
    elif isinstance(step,MatrixTransform):
        a,b,c,d,e,f,g,j,i=step.forward_matrix
        points=[];denominators=[]
        for x,y in ((x0-.5,y0-.5),(x1-.5,y0-.5),(x0-.5,y1-.5),(x1-.5,y1-.5)):
            denominator=g*x+j*y+i;denominators.append(denominator)
            if not math.isfinite(denominator) or abs(denominator)<1e-12:raise ValueError('REGION_PROJECTION_HORIZON')
            points.append(((a*x+b*y+c)/denominator+.5,(d*x+e*y+f)/denominator+.5))
        if min(denominators)<=0<=max(denominators):raise ValueError('REGION_PROJECTION_HORIZON')
        x0=min(p[0] for p in points);x1=max(p[0] for p in points)
        y0=min(p[1] for p in points);y1=max(p[1] for p in points)
    elif isinstance(step,DenseWarpTransform):
        if (inverse_field is None or inverse_field.dtype!=np.float32 or inverse_field.shape!=(h,w,2)
                or step.pixel_centers is not None):raise ValueError('REGION_PROJECTION_FIELD_INVALID')
        left=w;top=h;right=bottom=0
        for row in range(0,h,256):
            for column in range(0,w,256):
                check_active();field=inverse_field[row:row+256,column:column+256]
                if not np.isfinite(field).all():raise ValueError('REGION_PROJECTION_FIELD_INVALID')
                sx=np.clip(field[...,0],0,sw-1);sy=np.clip(field[...,1],0,sh-1)
                # Every nonzero bilinear contributor with replicated borders.
                # Horizontal tiling bounds intermediates even for wide panoramas.
                hit=(np.floor(sx)<x1)&(np.ceil(sx)>=x0)&(np.floor(sy)<y1)&(np.ceil(sy)>=y0)
                rows,cols=np.nonzero(hit)
                if rows.size:
                    left=min(left,column+int(cols.min()));right=max(right,column+int(cols.max())+1)
                    top=min(top,row+int(rows.min()));bottom=max(bottom,row+int(rows.max())+1)
        if right<=left or bottom<=top:raise ValueError('REGION_OUTSIDE_RENDERED_CANVAS')
        return ProjectedBounds(left,top,right-left,bottom-top)
    else:raise ValueError('REGION_PROJECTION_TRANSFORM_UNSUPPORTED')
    left=max(0,math.floor(x0));top=max(0,math.floor(y0));right=min(w,math.ceil(x1));bottom=min(h,math.ceil(y1))
    if right<=left or bottom<=top:raise ValueError('REGION_OUTSIDE_RENDERED_CANVAS')
    return ProjectedBounds(left,top,right-left,bottom-top)


def project_region_chain(bounds,source_canvas,target_canvas,source_chain,target_chain,*,
                         load_inverse_field,check_active=lambda:None):
    """Append-only chain projection with retained authenticated field lifetimes.

    The loader is task-scoped artifact IO, not an optional qualification callback.
    It must verify declared input/output membership and hash before mapping.
    Prefix replacement, stale canvases and missing dense evidence fail closed.
    """
    source_chain.validate_target(source_canvas);target_chain.validate_target(target_canvas)
    prefix=source_chain.steps
    if target_chain.steps[:len(prefix)]!=prefix:raise ValueError('REGION_PROJECTION_CHAIN_REPLACED')
    steps=target_chain.steps[len(prefix):]
    current=source_canvas
    for step in steps:
        check_active()
        if step.from_canvas!=current:raise ValueError('REGION_PROJECTION_CHAIN_DISCONTINUOUS')
        if isinstance(step,DenseWarpTransform):
            with load_inverse_field(step.inverse_field,step.to_canvas) as field:
                bounds=project_region_bounds(bounds,step,inverse_field=field,check_active=check_active)
        else:bounds=project_region_bounds(bounds,step,check_active=check_active)
        current=step.to_canvas
    if current!=target_canvas:raise ValueError('REGION_PROJECTION_TARGET_INVALID')
    check_active()
    return bounds

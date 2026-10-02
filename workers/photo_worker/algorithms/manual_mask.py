"""Source-coordinate vector rasterization without full-frame RAM buffers."""
from dataclasses import dataclass
from collections.abc import Iterator,Callable
import numpy as np
from photo_worker.contracts.manual_mask import ManualMask,Stroke

@dataclass(frozen=True)
class MaskTile:
    x:int
    y:int
    target:np.ndarray
    protected:np.ndarray


def _stroke(points,x,y,radius,hardness,check_active):
    distance=np.full(np.broadcast_shapes(x.shape,y.shape),np.inf)
    segments=zip(points,points[1:]) if len(points)>1 else [(points[0],points[0])]
    for a,b in segments:
        check_active()
        dx,dy=b[0]-a[0],b[1]-a[1]
        denominator=dx*dx+dy*dy
        t=np.clip(((x-a[0])*dx+(y-a[1])*dy)/denominator,0,1) if denominator else 0
        distance=np.minimum(distance,np.hypot(x-(a[0]+t*dx),y-(a[1]+t*dy)))
    # One-pixel analytic edge ramp; hardness controls the inner constant disk.
    inner=radius*hardness
    if hardness==1:return np.clip(radius+.5-distance,0,1).astype(np.float32)
    t=np.clip((radius-distance)/max(radius-inner,np.finfo(float).eps),0,1)
    return (t*t*(3-2*t)).astype(np.float32)


def _polygon(points,x,y,check_active):
    coverage=np.zeros(np.broadcast_shapes(x.shape,y.shape),np.float32)
    for ox,oy in ((-.25,-.25),(.25,-.25),(-.25,.25),(.25,.25)):
        inside=np.zeros(coverage.shape,bool);px=x+ox;py=y+oy
        for a,b in zip(points,points[1:]+points[:1]):
            check_active()
            if a[1]==b[1]:continue
            crosses=((a[1]>py)!=(b[1]>py)) & (px < (b[0]-a[0])*(py-a[1])/(b[1]-a[1])+a[0])
            inside ^= crosses
        coverage+=inside.astype(np.float32)*.25
    return coverage


def rasterize(mask:ManualMask,source_sha256:str,*,tile_size:int=512,
              check_active:Callable[[],None]=lambda:None)->Iterator[MaskTile]:
    """Yield owned readonly target/protection tiles; consumer stages PNG16.

    Shapes use source edge coordinates, pixel centers(x+.5,y+.5). Add/protect
    are union=max; subtract multiplies target by(1-coverage). Protect never
    destroys editable target data, and is composed at effect application.
    """
    check_active();mask.coherent()
    if mask.source_sha256!=source_sha256:raise ValueError('OVERRIDE_SOURCE_MISMATCH')
    if type(tile_size) is not int or tile_size not in (256,512,1024):raise ValueError('Unsupported mask tile size')
    h,w=mask.canvas.height,mask.canvas.width
    shapes=[]
    for shape in sorted(mask.shapes,key=lambda s:s.sequence):
        points=[(p.x*w,p.y*h) for p in shape.points]
        radius=shape.radius*max(w,h) if isinstance(shape,Stroke) else 0
        xs,ys=zip(*points)
        shapes.append((shape,points,radius,(min(xs)-radius-1,min(ys)-radius-1,max(xs)+radius+1,max(ys)+radius+1)))
    for y in range(0,h,tile_size):
        for x in range(0,w,tile_size):
            check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x)
            target=np.zeros((th,tw),np.float32);protected=np.zeros_like(target)
            px=np.arange(x,x+tw,dtype=np.float64)[None,:]+.5
            py=np.arange(y,y+th,dtype=np.float64)[:,None]+.5
            for shape,points,radius,bounds in shapes:
                check_active()
                if bounds[2]<x or bounds[3]<y or bounds[0]>x+tw or bounds[1]>y+th:continue
                coverage=(_stroke(points,px,py,radius,shape.hardness,check_active) if isinstance(shape,Stroke)
                          else _polygon(points,px,py,check_active))
                if shape.mode=='add':np.maximum(target,coverage,out=target)
                elif shape.mode=='subtract':target*=1-coverage
                else:np.maximum(protected,coverage,out=protected)
            target.flags.writeable=False;protected.flags.writeable=False
            yield MaskTile(x,y,target,protected)

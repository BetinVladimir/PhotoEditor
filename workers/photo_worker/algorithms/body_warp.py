"""Bounded inverse body warp. Fields are absolute output-to-input pixel centres."""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.body_fit import BodyCoefficients
from photo_worker.contracts.images import Face68,Pose17


@dataclass(frozen=True)
class WarpValidation:
    safe:bool
    max_displacement:float
    min_jacobian:float
    protected_center_displacement:float


def _pixel(point,w,h):return np.array([point.x*(w-1),point.y*(h-1)],np.float64)


def anchors(pose:Pose17,coeff:BodyCoefficients,strength:float,w:int,h:int)->tuple[np.ndarray,np.ndarray,np.ndarray]:
    """Return target anchors, inverse displacements, and a conservative person ROI."""
    p=np.stack([_pixel(v,w,h) for v in pose.points]);conf=np.array([v.confidence for v in pose.points])
    required=[5,6,11,12,13,14,15,16]
    if np.min(conf[required])<.35:raise ValueError('MISSING_BODY_ANCHORS')
    left=min(p[5,0],p[11,0],p[13,0],p[15,0]);right=max(p[6,0],p[12,0],p[14,0],p[16,0])
    top=min(p[5,1],p[6,1]);bottom=max(p[15,1],p[16,1]);width=max(2.,right-left);height=max(2.,bottom-top)
    center=(p[11,0]+p[12,0])/2
    out=[];disp=[]
    def pair(li,ri,amount):
        for idx,sign in ((li,-1),(ri,1)):
            q=p[idx].copy();d=np.array([sign*width*amount*strength,0.],np.float64);out.append(q-d);disp.append(d)
    pair(5,6,coeff.torso_slim);pair(11,12,coeff.hip_slim)
    waist_y=(p[5,1]+p[6,1]+p[11,1]+p[12,1])/4
    for sign in (-1,1):
        q=np.array([center+sign*width*.38,waist_y]);d=np.array([sign*width*coeff.waist_slim*strength,0.]);out.append(q-d);disp.append(d)
    for idx in (15,16):
        d=np.array([0.,height*coeff.leg_length*strength]);out.append(p[idx]-d);disp.append(d)
    # Boundary anchors constrain the field to exact identity outside the person ROI.
    margin=.18*max(width,height);x0=max(0,left-margin);x1=min(w-1,right+margin);y0=max(0,top-margin);y1=min(h-1,bottom+margin)
    for x,y in ((x0,y0),(x1,y0),(x0,y1),(x1,y1),((x0+x1)/2,y0),((x0+x1)/2,y1),(x0,(y0+y1)/2),(x1,(y0+y1)/2)):
        out.append(np.array([x,y]));disp.append(np.zeros(2))
    return np.stack(out),np.stack(disp),np.array([x0,y0,x1,y1],np.float64)


def face_anchors(face:Face68,amount:float,strength:float,w:int,h:int)->tuple[np.ndarray,np.ndarray]:
    p=np.stack([_pixel(v,w,h) for v in face.points]);jaw=p[:17]
    if min(v.confidence for v in face.points[:17])<.35:raise ValueError('MISSING_FACE_ANCHORS')
    center=(jaw[0]+jaw[16])/2;width=max(2.,jaw[16,0]-jaw[0,0]);out=[];disp=[]
    for idx in range(17):
        q=jaw[idx];direction=np.sign(q[0]-center[0]);d=np.array([direction*width*amount*strength,0.]);out.append(q-d);disp.append(d)
    return np.stack(out),np.stack(disp)


def solve_inverse_field(width:int,height:int,sets:list[tuple[np.ndarray,np.ndarray,np.ndarray]],person:np.ndarray)->np.ndarray:
    yy,xx=np.mgrid[0:height,0:width];field=np.stack((xx,yy),axis=-1).astype(np.float32)
    coverage=np.zeros((height,width),np.uint8)
    for points,values,roi in sets:
        x0,y0,x1,y1=np.floor(roi).astype(int);x1=min(width-1,int(np.ceil(roi[2])));y1=min(height-1,int(np.ceil(roi[3])))
        if x1<x0 or y1<y0:continue
        sigma=max(.2*max(x1-x0+1,y1-y0+1),1.);delta=points[:,None,:]-points[None,:,:]
        kernel=np.exp(-np.sum(delta*delta,axis=2)/(2*sigma*sigma));weights=np.linalg.solve(kernel+np.eye(len(points))*1e-4,values)
        ry,rx=np.mgrid[y0:y1+1,x0:x1+1];query=np.stack((rx,ry),axis=-1);d=query[...,None,:]-points
        moved=np.exp(-np.sum(d*d,axis=-1)/(2*sigma*sigma))@weights
        gate=np.clip(person[y0:y1+1,x0:x1+1,...],0,1)[...,None]
        active=np.linalg.norm(moved,axis=-1)>.05
        prior=coverage[y0:y1+1,x0:x1+1]
        if np.any(active&(prior>0)):raise ValueError('AMBIGUOUS_OVERLAP')
        field[y0:y1+1,x0:x1+1]+=np.asarray(moved*gate,np.float32);coverage[y0:y1+1,x0:x1+1]+=active
    return field


def validate_field(field:np.ndarray,max_displacement:float,person:np.ndarray)->WarpValidation:
    h,w,_=field.shape;yy,xx=np.mgrid[0:h,0:w];dx=field[...,0]-xx;dy=field[...,1]-yy
    maximum=float(np.max(np.hypot(dx,dy)))
    ys=np.unique(np.r_[np.arange(0,h,4),h-1]);xs=np.unique(np.r_[np.arange(0,w,4),w-1])
    # Central differences of the inverse map. A positive lower bound excludes folds.
    gx_x=np.gradient(field[...,0],axis=1);gx_y=np.gradient(field[...,0],axis=0)
    gy_x=np.gradient(field[...,1],axis=1);gy_y=np.gradient(field[...,1],axis=0)
    det=gx_x*gy_y-gx_y*gy_x;minimum=float(np.min(det[np.ix_(ys,xs)]))
    protected=person<.01;center=float(np.max(np.hypot(dx[protected],dy[protected]))) if np.any(protected) else 0.
    protected_limit=.1*max(h,w)/1024
    return WarpValidation(maximum<=max_displacement and minimum>=.5 and center<=protected_limit,maximum,minimum,center)


def bilinear(source:np.ndarray,field:np.ndarray)->np.ndarray:
    h,w=field.shape[:2];sx=np.clip(field[...,0],0,w-1);sy=np.clip(field[...,1],0,h-1)
    x0=np.floor(sx).astype(np.int64);y0=np.floor(sy).astype(np.int64);x1=np.minimum(x0+1,w-1);y1=np.minimum(y0+1,h-1)
    fx=sx-x0;fy=sy-y0
    if source.ndim==3:fx=fx[...,None];fy=fy[...,None]
    return ((source[y0,x0]*(1-fx)+source[y0,x1]*fx)*(1-fy)+(source[y1,x0]*(1-fx)+source[y1,x1]*fx)*fy)

"""Deterministic person association and bounded body-warp coefficient fitting."""
from __future__ import annotations
from dataclasses import dataclass
import numpy as np
from scipy.optimize import linear_sum_assignment


COCO=(0,5,6,11,12,13,14,15,16)


@dataclass(frozen=True)
class Match:
    before:int;after:int;cost:float;iou:float;landmark_error:float;margin:float


def _points(pose):
    return np.array([[p.x,p.y,p.confidence] for p in pose.points],np.float64)


def transform_points(points,matrix,before_canvas,after_canvas):
    xy=points[:,:2]*[before_canvas.width,before_canvas.height]
    homogeneous=np.column_stack((xy,np.ones(len(xy))))@np.asarray(matrix,np.float64).reshape(3,3).T
    if np.any(np.abs(homogeneous[:,2])<1e-12):raise ValueError('BODY_ALIGNMENT_INVALID')
    result=homogeneous[:,:2]/homogeneous[:,2,None];result/=np.array([after_canvas.width,after_canvas.height])
    return np.column_stack((result,points[:,2]))


def _bbox(points):
    valid=points[:,2]>=.5
    if np.count_nonzero(valid)<5:return None
    xy=points[valid,:2];return np.array([xy[:,0].min(),xy[:,1].min(),xy[:,0].max(),xy[:,1].max()],np.float64)


def _iou(a,b):
    if a is None or b is None:return 0.
    inter=max(0,min(a[2],b[2])-max(a[0],b[0]))*max(0,min(a[3],b[3])-max(a[1],b[1]))
    union=max(0,(a[2]-a[0])*(a[3]-a[1]))+max(0,(b[2]-b[0])*(b[3]-b[1]))-inter
    return inter/union if union>0 else 0.


def _error(a,b):
    valid=(a[:,2]>=.5)&(b[:,2]>=.5)
    if np.count_nonzero(valid)<5:return 1.
    box=_bbox(b);diag=np.linalg.norm(box[2:]-box[:2]) if box is not None else 0
    return min(1.,float(np.mean(np.linalg.norm(a[valid,:2]-b[valid,:2],axis=1))/max(diag,1e-6)))


def match_poses(before,after,matrix,before_canvas,after_canvas):
    if not before or not after:return []
    a=[transform_points(_points(p),matrix,before_canvas,after_canvas) for p in before];b=[_points(p) for p in after]
    cost=np.empty((len(a),len(b)),np.float64);ious=np.empty_like(cost);errors=np.empty_like(cost)
    for i,x in enumerate(a):
        for j,y in enumerate(b):
            ious[i,j]=_iou(_bbox(x),_bbox(y));errors[i,j]=_error(x,y);cost[i,j]=.5*(1-ious[i,j])+.5*errors[i,j]
    rows,cols=linear_sum_assignment(cost);matches=[]
    for i,j in zip(rows,cols):
        alternatives=[cost[i,k] for k in range(len(b)) if k!=j]+[cost[k,j] for k in range(len(a)) if k!=i]
        margin=min(alternatives)-cost[i,j] if alternatives else 1.
        matches.append(Match(int(i),int(j),float(np.clip(cost[i,j],0,1)),float(np.clip(ious[i,j],0,1)),float(errors[i,j]),float(np.clip(margin,0,1))))
    return matches


def silhouette_width(mask,y,x0,x1):
    h,w=mask.shape;row=int(round(np.clip(y,0,1)*(h-1)));lo=max(0,int(np.floor(x0*w)));hi=min(w,int(np.ceil(x1*w)))
    widths=[]
    for yy in range(max(0,row-max(1,h//100)),min(h,row+max(1,h//100)+1)):
        xs=np.flatnonzero(mask[yy,lo:hi]>=.5)
        if len(xs):widths.append((xs[-1]-xs[0]+1)/w)
    return float(np.median(widths)) if widths else 0.


def pose_measurements(pose,mask):
    p=_points(pose);needed=[5,6,11,12,15,16]
    if any(p[i,2]<.5 for i in needed):return None
    shoulder=(p[5,:2]+p[6,:2])/2;hip=(p[11,:2]+p[12,:2])/2
    bbox=_bbox(p);x0=max(0,bbox[0]-.1);x1=min(1,bbox[2]+.1)
    torso=silhouette_width(mask,float(.45*shoulder[1]+.55*hip[1]),x0,x1)
    waist=silhouette_width(mask,float(.25*shoulder[1]+.75*hip[1]),x0,x1)
    hips=silhouette_width(mask,float(hip[1]),x0,x1)
    leg=(np.linalg.norm(p[11,:2]-p[15,:2])+np.linalg.norm(p[12,:2]-p[16,:2]))/2
    if min(torso,waist,hips,leg)<=1e-6:return None
    return np.array([torso,waist,hips,leg],np.float64)


def bounded_coefficients(before,after,width_scale,length_scale):
    b=before.copy();b[:3]*=width_scale;b[3]*=length_scale
    slim=np.maximum(0,(b[:3]-after[:3])/(2*np.maximum(b[:3],1e-6)))
    leg=max(0,(after[3]-b[3])/max(b[3],1e-6))
    return np.array([min(.08,slim[0]),min(.08,slim[1]),min(.08,slim[2]),min(.03,leg)],np.float64)

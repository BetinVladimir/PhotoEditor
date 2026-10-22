"""Bounded classical scene checks, not source authority or qualification.

Inputs are installer-owned, hash-verified same-grid proxies. The caller proves
canonical scene/depth producers and policy provenance independently. No default
background labels or safety thresholds are supplied here.
"""
from dataclasses import dataclass
import math
import cv2
import numpy as np
from .semantic_transfer import _RANSAC_LOCK,validate_planar_homography


@dataclass(frozen=True)
class ScenePolicy:
    background_labels: frozenset[int]
    min_support: int
    flow_error_px: float
    homography_error_px: float
    depth_plane_error: float

    def __post_init__(self):
        if (not isinstance(self.background_labels,frozenset) or not self.background_labels
                or any(type(v) is not int or not 0<=v<150 for v in self.background_labels)
                or type(self.min_support) is not int or not 32<=self.min_support<=1024**2
                or any(type(v) not in (float,int) or not math.isfinite(v) or v<=0
                       for v in (self.flow_error_px,self.homography_error_px,self.depth_plane_error))):
            raise ValueError('SEMANTIC_SCENE_POLICY_INVALID')


def _plane(depth,domain,check_active):
    # Inputs are capped at1024². Solve only a deterministic <=4096 sample system,
    # then check every supported pixel in bounded rows; no full-frame designmatrix.
    yy,xx=np.nonzero(domain);check_active()
    if len(xx)<32:raise ValueError('SEMANTIC_SCENE_SUPPORT_INSUFFICIENT')
    step=max(1,(len(xx)+4095)//4096);x=xx[::step];y=yy[::step]
    design=np.column_stack((x/max(1,depth.shape[1]-1),y/max(1,depth.shape[0]-1),np.ones(len(x))))
    coefficients,_,rank,_=np.linalg.lstsq(design,depth[y,x],rcond=None);check_active()
    if rank!=3:raise ValueError('SEMANTIC_SCENE_DEPTH_DEGENERATE')
    worst=0.
    for start in range(0,len(xx),65536):
        check_active();x=xx[start:start+65536];y=yy[start:start+65536]
        predicted=coefficients[0]*x/max(1,depth.shape[1]-1)+coefficients[1]*y/max(1,depth.shape[0]-1)+coefficients[2]
        worst=max(worst,float(np.max(np.abs(depth[y,x]-predicted))))
    check_active();return worst


def verify_planar_scene_proxies(reference,target,reference_labels,target_labels,
        reference_depth,target_depth,source_mask,source_protected,target_protected,
        matrix,*,policy:ScenePolicy,check_active=lambda:None):
    """Conservative positive-support gate. Raises on any unsupported pixel.

    This checks reproducible scene evidence; it does not claim real-world plane
    truth or calibrate thresholds. All evidence must use this exact proxy grid.
    """
    check_active()
    if not isinstance(policy,ScenePolicy):raise ValueError('SEMANTIC_SCENE_POLICY_INVALID')
    if (not isinstance(reference,np.ndarray) or reference.dtype!=np.uint8 or reference.ndim!=2
            or any(not 32<=v<=1024 for v in reference.shape)):
        raise ValueError('SEMANTIC_SCENE_PROXY_INVALID')
    shape=reference.shape
    if not isinstance(target,np.ndarray) or target.shape!=shape or target.dtype!=np.uint8:
        raise ValueError('SEMANTIC_SCENE_PROXY_INVALID')
    # Existing scene.parse publishes little-endian uint16 ADE labels. Never
    # narrow before checking class bounds (uint8 wrap would invent a class).
    for value in (reference_labels,target_labels):
        if (not isinstance(value,np.ndarray) or value.shape!=shape
                or value.dtype not in (np.dtype('uint8'),np.dtype('<u2'))
                or np.any(value>=150)):
            raise ValueError('SEMANTIC_SCENE_LABELS_INVALID')
    for value in (reference_depth,target_depth,source_mask,source_protected,target_protected):
        if (not isinstance(value,np.ndarray) or value.shape!=shape or value.dtype!=np.float32
                or not np.isfinite(value).all() or np.any(value<0) or np.any(value>1)):
            raise ValueError('SEMANTIC_SCENE_ARRAY_INVALID')
        check_active()
    matrix=np.asarray(matrix,dtype=np.float64)
    if not validate_planar_homography(matrix,shape):raise ValueError('SEMANTIC_SCENE_TRANSFORM_INVALID')
    domain=source_mask>0
    if np.count_nonzero(domain)<policy.min_support:raise ValueError('SEMANTIC_SCENE_SUPPORT_INSUFFICIENT')
    if np.any(domain & (source_protected>0)):raise ValueError('SEMANTIC_SCENE_PROTECTED')
    labels=np.fromiter(sorted(policy.background_labels),dtype=np.uint8)
    if np.any(domain & ~np.isin(reference_labels,labels)):
        raise ValueError('SEMANTIC_SCENE_FOREGROUND')
    if _plane(reference_depth,domain,check_active)>policy.depth_plane_error:
        raise ValueError('SEMANTIC_SCENE_NONPLANAR')
    # Shared OpenCV lock prevents mutation of global thread settings by parallel
    # ORB operations; restore previous setting even on cancellation/failure.
    with _RANSAC_LOCK:
        threads=cv2.getNumThreads()
        try:
            cv2.setNumThreads(1);check_active()
            forward=cv2.calcOpticalFlowFarneback(reference,target,None,.5,3,15,3,5,1.2,0)
            check_active()
            backward=cv2.calcOpticalFlowFarneback(target,reference,None,.5,3,15,3,5,1.2,0)
            check_active()
        finally:cv2.setNumThreads(threads)
    if not np.isfinite(forward).all() or not np.isfinite(backward).all():
        raise ValueError('SEMANTIC_SCENE_FLOW_INVALID')
    target_domain=np.zeros(shape,dtype=bool)
    for top in range(0,shape[0],64):
        check_active();y,x=np.nonzero(domain[top:top+64]);y=y+top
        if not len(x):continue
        q=np.column_stack((x,y,np.ones(len(x))))@matrix.T
        if not np.isfinite(q).all() or np.any(np.abs(q[:,2])<=1e-12):
            raise ValueError('SEMANTIC_SCENE_TRANSFORM_INVALID')
        q=q[:,:2]/q[:,2,None]
        if not np.isfinite(q).all():raise ValueError('SEMANTIC_SCENE_TRANSFORM_INVALID')
        moved=np.column_stack((x,y))+forward[y,x]
        if (np.any(np.linalg.norm(moved-q,axis=1)>policy.homography_error_px)
                or np.any(moved[:,0]<0) or np.any(moved[:,1]<0)
                or np.any(moved[:,0]>shape[1]-1) or np.any(moved[:,1]>shape[0]-1)):
            raise ValueError('SEMANTIC_SCENE_PARALLAX_OR_OCCLUSION')
        # The published mask follows homography q, not the optical-flow
        # estimate. Gate contributors for BOTH footprints; threshold tolerance
        # never authorizes the actual warp to cross a protected/foreground edge.
        for coordinates in (q,moved):
            if (np.any(coordinates[:,0]<0) or np.any(coordinates[:,1]<0)
                    or np.any(coordinates[:,0]>shape[1]-1) or np.any(coordinates[:,1]>shape[0]-1)):
                raise ValueError('SEMANTIC_SCENE_PARALLAX_OR_OCCLUSION')
            cx0=np.floor(coordinates[:,0]).astype(np.int32);cy0=np.floor(coordinates[:,1]).astype(np.int32)
            cx1=np.minimum(cx0+1,shape[1]-1);cy1=np.minimum(cy0+1,shape[0]-1)
            for sx,sy in ((cx0,cy0),(cx1,cy0),(cx0,cy1),(cx1,cy1)):
                if np.any(target_protected[sy,sx]>0):raise ValueError('SEMANTIC_SCENE_PROTECTED')
                if not np.isin(target_labels[sy,sx],labels).all():raise ValueError('SEMANTIC_SCENE_FOREGROUND')
                target_domain[sy,sx]=True
        x0=np.floor(moved[:,0]).astype(np.int32);y0=np.floor(moved[:,1]).astype(np.int32)
        x1=np.minimum(x0+1,shape[1]-1);y1=np.minimum(y0+1,shape[0]-1)
        wx=(moved[:,0]-x0)[:,None];wy=(moved[:,1]-y0)[:,None]
        reverse=(backward[y0,x0]*(1-wx)*(1-wy)+backward[y0,x1]*wx*(1-wy)
                 +backward[y1,x0]*(1-wx)*wy+backward[y1,x1]*wx*wy)
        if np.any(np.linalg.norm(forward[y,x]+reverse,axis=1)>policy.flow_error_px):
            raise ValueError('SEMANTIC_SCENE_PARALLAX_OR_OCCLUSION')
    if _plane(target_depth,target_domain,check_active)>policy.depth_plane_error:
        raise ValueError('SEMANTIC_SCENE_NONPLANAR')
    check_active()
    return target_domain

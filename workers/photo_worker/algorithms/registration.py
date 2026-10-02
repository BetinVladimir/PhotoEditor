from __future__ import annotations
from dataclasses import dataclass
import threading
from typing import Callable
import cv2
import numpy as np

_RANSAC_LOCK=threading.Lock()

@dataclass(frozen=True)
class PairAlignment:
    before_to_after: np.ndarray
    overlap: float
    median_reprojection: float | None
    inlier_count: int
    confidence: str
    crop_aware: bool
    warnings: tuple[str,...]


def _image(rgb,check_active:Callable[[],None]=lambda:None):
    array=np.asarray(rgb,dtype=np.float32)
    if array.ndim!=3 or array.shape[-1]!=3 or min(array.shape[:2])<8:
        raise ValueError('PAIR_IMAGE_INVALID')
    # Attempt arrays are contiguous mappings; bounded slices avoid full-frame
    # boolean temporaries, including extremely wide images.
    if array.flags.c_contiguous:
        flat=array.reshape(-1)
        for start in range(0,flat.size,262144):
            check_active()
            if not np.isfinite(flat[start:start+262144]).all():raise ValueError('PAIR_IMAGE_INVALID')
    else:
        for y in range(array.shape[0]):
            for x in range(0,array.shape[1],65536):
                check_active()
                if not np.isfinite(array[y,x:x+65536]).all():raise ValueError('PAIR_IMAGE_INVALID')
    return array


def _preview(image):
    h,w=image.shape[:2]
    scale=min(1.,1024/max(h,w));width,height=max(8,round(w*scale)),max(8,round(h*scale))
    result=cv2.resize(image,(width,height),interpolation=cv2.INTER_AREA) if (height,width)!=(h,w) else image
    sx,sy=width/w,height/h
    transform=np.array([[sx,0,(sx-1)/2],[0,sy,(sy-1)/2],[0,0,1]],dtype=np.float64)
    gray=cv2.cvtColor(np.round(np.clip(result,0,1)*255).astype(np.uint8),cv2.COLOR_RGB2GRAY)
    return gray,transform


def _overlap(matrix,source_shape,target_shape):
    valid=cv2.warpAffine(np.ones(source_shape,dtype=np.uint8),matrix[:2],(target_shape[1],target_shape[0]),flags=cv2.INTER_NEAREST,borderMode=cv2.BORDER_CONSTANT,borderValue=0)
    return float(valid.mean()),valid


def _similarity(matrix):
    m=np.asarray(matrix,dtype=np.float64)
    if m.shape!=(3,3) or not np.isfinite(m).all() or not np.allclose(m[2],[0,0,1],atol=1e-9):
        raise ValueError('ALIGNMENT_MATRIX_INVALID')
    linear=m[:2,:2]
    if np.linalg.det(linear)<=0 or not np.allclose(linear.T@linear,np.eye(2)*np.trace(linear.T@linear)/2,rtol=1e-4,atol=1e-6):
        raise ValueError('ALIGNMENT_NOT_SIMILARITY')
    if not .1<=np.sqrt(np.linalg.det(linear))<=10:
        raise ValueError('ALIGNMENT_SCALE_INVALID')
    return m


def _gradient(gray):
    f=gray.astype(np.float32)/255
    magnitude=cv2.magnitude(cv2.Sobel(f,cv2.CV_32F,1,0,ksize=3),cv2.Sobel(f,cv2.CV_32F,0,1,ksize=3))
    return magnitude/max(float(np.percentile(magnitude,99)),1e-5)


def register_pair(before,after,*,seed:int=0,crop_aware:bool=False,
                  manual_matrix:np.ndarray|None=None,background_mask:np.ndarray|None=None,check_active:Callable[[],None]=lambda:None) -> PairAlignment:
    check_active()
    before,after=_image(before,check_active),_image(after,check_active)
    check_active()
    a,sa=_preview(before);b,sb=_preview(after)
    minimum_overlap=.5 if crop_aware else .9
    if manual_matrix is not None:
        full=_similarity(manual_matrix)
        matrix=sb@full@np.linalg.inv(sa)
        overlap,_=_overlap(matrix,a.shape,b.shape)
        if overlap<minimum_overlap:raise ValueError('PAIR_OVERLAP_INSUFFICIENT')
        return PairAlignment(full,overlap,None,0,'user_confirmed',crop_aware,())
    mask=None
    if background_mask is not None:
        if background_mask.shape!=before.shape[:2]:
            raise ValueError('ALIGNMENT_MASK_INVALID')
        for row in range(background_mask.shape[0]):
            for column in range(0,background_mask.shape[1],262144):
                check_active()
                if not np.isfinite(background_mask[row,column:column+262144]).all():raise ValueError('ALIGNMENT_MASK_INVALID')
        mask=(cv2.resize(background_mask,(a.shape[1],a.shape[0]),interpolation=cv2.INTER_NEAREST)>.5).astype(np.uint8)*255
    check_active()
    sift=cv2.SIFT_create(nfeatures=4000)
    ka,da=sift.detectAndCompute(a,mask);kb,db=sift.detectAndCompute(b,None)
    check_active()
    if da is None or db is None or min(len(ka),len(kb))<12:
        raise ValueError('PAIR_FEATURES_INSUFFICIENT')
    matches=cv2.BFMatcher(cv2.NORM_L2).knnMatch(da,db,k=2)
    good=[pair[0] for pair in matches if len(pair)==2 and pair[0].distance<.75*pair[1].distance]
    if len(good)<12:raise ValueError('PAIR_MATCHES_INSUFFICIENT')
    source=np.array([ka[m.queryIdx].pt for m in good],np.float64)
    target=np.array([kb[m.trainIdx].pt for m in good],np.float64)
    # OpenCV's RNG is process-global: serialize only the robust estimator.
    with _RANSAC_LOCK:
        cv2.setRNGSeed(int(seed)&0x7fffffff)
        affine,inliers=cv2.estimateAffinePartial2D(source,target,method=cv2.RANSAC,ransacReprojThreshold=1.5,maxIters=5000,confidence=.999,refineIters=10)
    check_active()
    if affine is None or inliers is None or int(inliers.sum())<12:
        raise ValueError('PAIR_ALIGNMENT_FAILED')
    matrix=np.vstack([affine,[0,0,1]])
    good_inliers=inliers.ravel().astype(bool)
    def reprojection(m):
        projected=source[good_inliers]@m[:2,:2].T+m[:2,2]
        return float(np.median(np.linalg.norm(projected-target[good_inliers],axis=1)))
    error=reprojection(matrix);overlap,valid=_overlap(matrix,a.shape,b.shape)
    warnings=[]
    initial=cv2.warpAffine(a,matrix[:2],(b.shape[1],b.shape[0]),flags=cv2.INTER_LINEAR)
    ecc_mask=cv2.erode(valid,np.ones((7,7),np.uint8))*255
    try:
        _,correction=cv2.findTransformECC(_gradient(b),_gradient(initial),np.eye(2,3,dtype=np.float32),cv2.MOTION_EUCLIDEAN,
                                        (cv2.TERM_CRITERIA_EPS|cv2.TERM_CRITERIA_COUNT,50,1e-6),ecc_mask,5)
        refined=np.linalg.inv(np.vstack([correction,[0,0,1]]))@matrix
        refined_error=reprojection(refined);refined_overlap,_=_overlap(refined,a.shape,b.shape)
        if refined_error<=error and refined_overlap>=minimum_overlap:
            matrix,error,overlap=refined,refined_error,refined_overlap
        else:warnings.append('ecc_not_improved')
    except cv2.error:
        warnings.append('ecc_not_converged')
    check_active()
    full=np.linalg.inv(sb)@matrix@sa
    # Integer preview dimensions introduce tiny anisotropic scaling. Project
    # back onto a true full-resolution similarity, preserving the inlier centre,
    # then recheck both quality gates using that actual published transform.
    u,singular,vt=np.linalg.svd(full[:2,:2])
    rotation=u@vt
    if np.linalg.det(rotation)<=0:raise ValueError('ALIGNMENT_NOT_SIMILARITY')
    linear=rotation*float(singular.mean())
    centre=np.linalg.inv(sa)@np.r_[source[good_inliers].mean(axis=0),1.]
    translation=(full@centre)[:2]-linear@centre[:2]
    full=_similarity(np.vstack([np.column_stack([linear,translation]),[0,0,1]]))
    matrix=sb@full@np.linalg.inv(sa)
    error=reprojection(matrix);overlap,_=_overlap(matrix,a.shape,b.shape)
    if overlap<minimum_overlap:raise ValueError('PAIR_OVERLAP_INSUFFICIENT')
    if error>1.:raise ValueError('PAIR_REPROJECTION_EXCEEDED')
    if background_mask is None:warnings.append('background_mask_not_provided')
    return PairAlignment(full,overlap,error,int(inliers.sum()),'measured',crop_aware,tuple(warnings))


def register_mtb_translation(before, after, *, check_active:Callable[[],None]=lambda:None) -> PairAlignment:
    """Exposure-invariant Median Threshold Bitmap fallback for HDR brackets."""
    check_active(); before,after=_image(before,check_active),_image(after,check_active)
    a,sa=_preview(before);b,sb=_preview(after)
    if a.shape!=b.shape:raise ValueError('MTB_CANVAS_MISMATCH')
    levels=min(6,max(0,int(np.floor(np.log2(min(a.shape)/16)))))
    pa=[a];pb=[b]
    for _ in range(levels):pa.append(cv2.pyrDown(pa[-1]));pb.append(cv2.pyrDown(pb[-1]))
    ga=_gradient(a);gb=_gradient(b);phase,response=cv2.phaseCorrelate(ga,gb)
    if not np.isfinite(phase).all() or not np.isfinite(response) or response<.05:phase=(0.,0.)
    shift=(0,0)
    for level in range(levels,-1,-1):
        check_active();source,target=pa[level],pb[level]
        scale=2**level;shift=(int(round(phase[0]/scale)),int(round(phase[1]/scale)))
        sm=float(np.median(source));tm=float(np.median(target))
        source_bits=source>sm;target_bits=target>tm;se=np.abs(source.astype(np.float32)-sm)>4;te=np.abs(target.astype(np.float32)-tm)>4
        best=None
        for dy in range(shift[1]-1,shift[1]+2):
            for dx in range(shift[0]-1,shift[0]+2):
                transform=np.array([[1,0,dx],[0,1,dy]],np.float32)
                shifted=cv2.warpAffine(source_bits.astype(np.uint8),transform,(target.shape[1],target.shape[0]),flags=cv2.INTER_NEAREST).astype(bool)
                valid=cv2.warpAffine(se.astype(np.uint8),transform,(target.shape[1],target.shape[0]),flags=cv2.INTER_NEAREST).astype(bool)&te
                count=int(valid.sum());score=float(np.count_nonzero((shifted^target_bits)&valid))/count if count else 1.
                candidate=(score,abs(dx)+abs(dy),dy,dx)
                if best is None or candidate<best:best=candidate
        shift=(best[3],best[2])
    matrix=np.array([[1.,0.,float(shift[0])],[0.,1.,float(shift[1])],[0.,0.,1.]],np.float64)
    overlap,_=_overlap(matrix,a.shape,b.shape)
    if overlap<.9:raise ValueError('PAIR_OVERLAP_INSUFFICIENT')
    full=_similarity(np.linalg.inv(sb)@matrix@sa)
    return PairAlignment(full,overlap,None,0,'measured',False,('mtb_fallback',))


def warp_pair_before(before,alignment:PairAlignment,target_shape:tuple[int,int]):
    """Explicit in-memory convenience; task adapters use warp_pair_tiles."""
    from .warp import warp_pair_tiles,validate_shape
    validate_shape(target_shape)
    output=np.empty((*target_shape,3),np.float32)
    valid=np.empty(target_shape,bool)
    for tile in warp_pair_tiles(before,alignment,target_shape):
        h,w=tile.valid.shape
        output[tile.y:tile.y+h,tile.x:tile.x+w]=tile.rgb
        valid[tile.y:tile.y+h,tile.x:tile.x+w]=tile.valid
    return output,valid

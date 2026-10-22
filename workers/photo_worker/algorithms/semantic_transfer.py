"""Calibrated planar correspondence gate; no pixel copying or approval."""
from dataclasses import dataclass
from math import isfinite
import cv2
import numpy as np
from .registration import _RANSAC_LOCK


@dataclass(frozen=True)
class PlanarMatch:
    reference_to_target: np.ndarray | None
    inlier_fraction: float
    reason: str | None


def validate_planar_homography(matrix,shape):
    """Reject singular, reflected or horizon-crossing source mappings."""
    if (not isinstance(shape,tuple) or len(shape)!=2
            or any(type(v) is not int or v<8 for v in shape) or shape[0]*shape[1]>200000000):return False
    h,w=shape
    matrix=np.asarray(matrix,dtype=np.float64)
    if matrix.shape!=(3,3) or not np.isfinite(matrix).all() or abs(matrix[2,2])<1e-12:
        return False
    matrix=matrix/matrix[2,2]
    if abs(np.linalg.det(matrix))<1e-12:return False
    x,y=np.meshgrid(np.linspace(0,w-1,33),np.linspace(0,h-1,33))
    denominator=matrix[2,0]*x+matrix[2,1]*y+matrix[2,2]
    if np.any(denominator<=1e-8):return False
    return bool(np.all(np.linalg.det(matrix)/denominator**3>0))


def fit_planar_correspondences(reference,target,*,reference_shape,match_error_px_max,
        match_inlier_min,check_active=lambda:None):
    """Proxy-coordinate RANSAC, fixed seed, confidence is observed inlier ratio.

    Caller supplies qualified ORB correspondences on a verified planar domain.
    Geometry alone cannot establish absence of parallax/occlusion or authority.
    """
    check_active()
    if (not isinstance(reference_shape,tuple) or len(reference_shape)!=2
            or any(type(v) is not int or not 8<=v<=2048 for v in reference_shape)
            or isinstance(match_error_px_max,bool) or not isinstance(match_error_px_max,(int,float))
            or not isfinite(match_error_px_max) or not 0<match_error_px_max<=100
            or isinstance(match_inlier_min,bool) or not isinstance(match_inlier_min,(int,float))
            or not isfinite(match_inlier_min) or not 0<=match_inlier_min<=1):
        raise ValueError('SEMANTIC_MATCH_CONFIG_INVALID')
    a=np.array(reference,dtype=np.float64,copy=True);b=np.array(target,dtype=np.float64,copy=True)
    if (a.ndim!=2 or a.shape[1:]!=(2,) or a.shape!=b.shape or len(a)>4000
            or not np.isfinite(a).all() or not np.isfinite(b).all()):
        raise ValueError('SEMANTIC_MATCH_INPUT_INVALID')
    if len(a)<12 or any(np.linalg.matrix_rank(points-points.mean(axis=0))<2 for points in (a,b)):
        return PlanarMatch(None,0.,'NO_PLANAR_CORRESPONDENCE')
    with _RANSAC_LOCK:
        check_active()
        threads=cv2.getNumThreads()
        try:
            cv2.setNumThreads(1);cv2.setRNGSeed(2026)
            matrix,inliers=cv2.findHomography(a,b,cv2.RANSAC,float(match_error_px_max),
                maxIters=2000,confidence=.995)
        finally:cv2.setNumThreads(threads)
    check_active()
    fraction=0. if inliers is None else float(np.count_nonzero(inliers)/len(a))
    if matrix is None or fraction<match_inlier_min:
        return PlanarMatch(None,fraction,'NO_PLANAR_CORRESPONDENCE')
    if not validate_planar_homography(matrix,reference_shape):
        return PlanarMatch(None,fraction,'UNSAFE_PLANAR_TRANSFORM')
    matrix=np.array(matrix/matrix[2,2],dtype=np.float64,copy=True);matrix.flags.writeable=False
    return PlanarMatch(matrix,fraction,None)


@dataclass(frozen=True)
class PlanarCorrespondences:
    reference: np.ndarray
    target: np.ndarray


def match_planar_proxies(reference,target,*,reference_mask=None,target_mask=None,check_active=lambda:None):
    """Locked ORB/BF matching on caller-verified linear-luma uint8 proxies.

    Proxy conversion, planar/protection evidence and full-canvas mapping belong
    to the operation adapter. This function never assumes a face is planar.
    """
    def owned(value):
        if (not isinstance(value,np.ndarray) or value.dtype!=np.uint8 or value.ndim!=2
                or any(not 8<=v<=2048 for v in value.shape)):
            raise ValueError('SEMANTIC_PROXY_INVALID')
        check_active()
        return np.array(value,copy=True,order='C')
    a,b=owned(reference),owned(target)
    def mask(value, shape):
        if value is None:return None
        if not isinstance(value,np.ndarray) or value.dtype!=np.uint8 or value.shape!=shape:
            raise ValueError('SEMANTIC_PROXY_MASK_INVALID')
        return np.array(value,copy=True,order='C')
    ma,mb=mask(reference_mask,a.shape),mask(target_mask,b.shape)
    with _RANSAC_LOCK:
        check_active();threads=cv2.getNumThreads()
        try:
            cv2.setNumThreads(1);cv2.setRNGSeed(2026)
            orb=cv2.ORB_create(nfeatures=4000,scaleFactor=1.2,nlevels=8,edgeThreshold=31,
                firstLevel=0,WTA_K=2,scoreType=cv2.ORB_HARRIS_SCORE,patchSize=31,fastThreshold=20)
            ka,da=orb.detectAndCompute(a,ma);check_active()
            kb,db=orb.detectAndCompute(b,mb);check_active()
            if da is None or db is None or len(da)<2 or len(db)<2:
                forward=reverse=[]
            else:
                matcher=cv2.BFMatcher(cv2.NORM_HAMMING,crossCheck=False)
                forward=matcher.knnMatch(da,db,k=2);check_active()
                reverse=matcher.knnMatch(db,da,k=2);check_active()
        finally:cv2.setNumThreads(threads)
    def accepted(rows):
        result={}
        for index,row in enumerate(rows):
            if index%256==0:check_active()
            if len(row)<2:continue
            first,second=sorted(row,key=lambda match:(match.distance,match.trainIdx,match.queryIdx))[:2]
            if first.distance<.75*second.distance:result[first.queryIdx]=first.trainIdx
        return result
    forward,reverse=accepted(forward),accepted(reverse)
    pairs=sorted((i,j) for i,j in forward.items() if reverse.get(j)==i)
    pa=np.array([ka[i].pt for i,j in pairs],dtype=np.float64).reshape(-1,2)
    pb=np.array([kb[j].pt for i,j in pairs],dtype=np.float64).reshape(-1,2)
    pa.flags.writeable=False;pb.flags.writeable=False;check_active()
    return PlanarCorrespondences(pa,pb)


@dataclass(frozen=True)
class PlanarProxy:
    pixels: np.ndarray
    source_to_proxy: np.ndarray


def planar_linear_proxy(rgb,*,check_active=lambda:None):
    """Rec2020 linear luma proxy, longest edge≤2048, explicit centre mapping.

    Caller admits retained native resize buffers and verifies WorkingImage/ICC.
    HDR clipping is for the matching proxy only; the source is never modified.
    """
    if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3
            or rgb.shape[2]!=3 or min(rgb.shape[:2])<8 or rgb.shape[0]*rgb.shape[1]>200000000):
        raise ValueError('SEMANTIC_PROXY_INPUT_INVALID')
    h,w=rgb.shape[:2]
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active()
            if not np.isfinite(rgb[y:y+256,x:x+256]).all():raise ValueError('SEMANTIC_PROXY_INPUT_INVALID')
    scale=min(1.,2048/max(h,w));pw,ph=max(8,round(w*scale)),max(8,round(h*scale))
    check_active()
    resized=cv2.resize(rgb,(pw,ph),interpolation=cv2.INTER_AREA) if (ph,pw)!=(h,w) else rgb
    gray=np.empty((ph,pw),np.uint8)
    coefficients=np.array([.2627,.6780,.0593],np.float64)
    for y in range(0,ph,256):
        for x in range(0,pw,256):
            check_active();part=np.s_[y:y+256,x:x+256]
            luma=np.sum(resized[part].astype(np.float64)*coefficients,axis=2)
            gray[part]=np.rint(np.clip(luma,0,1)*255).astype(np.uint8)
    sx,sy=pw/w,ph/h
    matrix=np.array([[sx,0,(sx-1)/2],[0,sy,(sy-1)/2],[0,0,1]],np.float64)
    gray.flags.writeable=False;matrix.flags.writeable=False
    check_active()
    return PlanarProxy(gray,matrix)


def match_planar_linear_images(reference,target,*,match_error_px_max,match_inlier_min,
        check_active=lambda:None):
    """Complete image matching geometry; scene/mask safety remains external."""
    a=planar_linear_proxy(reference,check_active=check_active)
    b=planar_linear_proxy(target,check_active=check_active)
    matched=match_planar_proxies(a.pixels,b.pixels,check_active=check_active)
    result=fit_planar_correspondences(matched.reference,matched.target,reference_shape=a.pixels.shape,
        match_error_px_max=match_error_px_max,match_inlier_min=match_inlier_min,check_active=check_active)
    if result.reference_to_target is None:return result
    matrix=np.linalg.inv(b.source_to_proxy)@result.reference_to_target@a.source_to_proxy
    if not validate_planar_homography(matrix,reference.shape[:2]):
        return PlanarMatch(None,result.inlier_fraction,'UNSAFE_PLANAR_TRANSFORM')
    matrix=np.array(matrix/matrix[2,2],copy=True);matrix.flags.writeable=False
    check_active()
    return PlanarMatch(matrix,result.inlier_fraction,None)


def warp_planar_mask_into(source,matrix,output,*,source_protected,target_protected,
        check_active=lambda:None):
    """Bilinear mask warp into an admitted private float32 destination.

    Caller discards the entire destination on any failure/cancellation and
    publishes only after successful return. Explicit protection arrays must
    come from verified source/target evidence; zero arrays are not authority.
    """
    def validate(value,shape=None):
        if (not isinstance(value,np.ndarray) or value.dtype!=np.float32 or value.ndim!=2
                or min(value.shape)<8 or value.size>200000000 or shape is not None and value.shape!=shape):
            raise ValueError('SEMANTIC_MASK_INVALID')
        for y in range(0,value.shape[0],256):
            for x in range(0,value.shape[1],256):
                check_active();part=value[y:y+256,x:x+256]
                if not np.isfinite(part).all() or np.any(part<0) or np.any(part>1):
                    raise ValueError('SEMANTIC_MASK_INVALID')
    validate(source);validate(source_protected,source.shape)
    if (not isinstance(output,np.ndarray) or output.dtype!=np.float32 or output.ndim!=2
            or min(output.shape)<8 or output.size>200000000 or not output.flags.writeable):
        raise ValueError('SEMANTIC_MASK_OUTPUT_INVALID')
    validate(target_protected,output.shape)
    if any(np.may_share_memory(output,value) for value in (source,source_protected,target_protected)):
        raise ValueError('SEMANTIC_MASK_OUTPUT_ALIASED')
    matrix=np.array(matrix,dtype=np.float64,copy=True)
    if not validate_planar_homography(matrix,source.shape):raise ValueError('UNSAFE_PLANAR_TRANSFORM')
    for y in range(0,source.shape[0],256):
        for x in range(0,source.shape[1],256):
            check_active();part=np.s_[y:y+256,x:x+256]
            if np.any((source[part]>0)&(source_protected[part]>0)):
                raise ValueError('SEMANTIC_PROTECTED_OVERLAP')
    for part,tile in iter_planar_mask_warp_tiles(source,matrix,output.shape,check_active=check_active):
        if np.any((tile>0)&(target_protected[part]>0)):
            raise ValueError('SEMANTIC_PROTECTED_OVERLAP')
        output[part]=tile
    check_active()


def iter_planar_mask_warp_tiles(source,matrix,target_shape,*,check_active=lambda:None):
    """The exact published mask sampler, also used for physical scene support.

    Source arrays are caller-validated; this iterator never publishes or relaxes
    source/protection authority. It retains only a256x256 destination tile.
    """
    if (not isinstance(source,np.ndarray) or source.dtype!=np.float32 or source.ndim!=2
            or not isinstance(target_shape,tuple) or len(target_shape)!=2
            or any(type(n) is not int or n<8 for n in target_shape)
            or target_shape[0]*target_shape[1]>200000000
            or not validate_planar_homography(matrix,source.shape)):
        raise ValueError('SEMANTIC_MASK_INVALID')
    check_active();inverse=np.linalg.inv(matrix);h,w=target_shape
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active();th,tw=min(256,h-y),min(256,w-x)
            yy,xx=np.mgrid[y:y+th,x:x+tw].astype(np.float64)
            denominator=inverse[2,0]*xx+inverse[2,1]*yy+inverse[2,2]
            with np.errstate(divide='ignore',invalid='ignore'):
                sx=(inverse[0,0]*xx+inverse[0,1]*yy+inverse[0,2])/denominator
                sy=(inverse[1,0]*xx+inverse[1,1]*yy+inverse[1,2])/denominator
            valid=np.isfinite(sx)&np.isfinite(sy)&(sx>-1)&(sy>-1)&(sx<source.shape[1])&(sy<source.shape[0])
            tile=np.zeros((th,tw),np.float32)
            if valid.any():
                left=max(0,int(np.floor(sx[valid].min())))
                top=max(0,int(np.floor(sy[valid].min())))
                right=min(source.shape[1],int(np.floor(sx[valid].max()))+2)
                bottom=min(source.shape[0],int(np.floor(sy[valid].max()))+2)
                if right-left>=32767 or bottom-top>=32767:raise ValueError('SEMANTIC_WARP_ROI_UNSUPPORTED')
                mx=np.where(valid,sx-left,-2).astype(np.float32)
                my=np.where(valid,sy-top,-2).astype(np.float32)
                tile=cv2.remap(source[top:bottom,left:right],mx,my,cv2.INTER_LINEAR,
                    borderMode=cv2.BORDER_CONSTANT,borderValue=0)
                tile[~valid]=0
                np.clip(tile,0,1,out=tile)
            part=np.s_[y:y+th,x:x+tw]
            yield part,tile
    check_active()

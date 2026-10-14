"""Bounded manual perspective geometry; coordinates are pixel-edge based.

Output samples are taken at half-pixel centers. The returned matrix maps
source edge coordinates into output edge coordinates and preserves HDR values.
"""
from dataclasses import dataclass
from collections.abc import Callable, Iterator
import numpy as np


@dataclass(frozen=True)
class PerspectiveGeometry:
    source_to_output: np.ndarray
    width: int
    height: int
    crop_fraction: float


def checked_homography(matrix, corners):
    h=np.array(matrix,dtype=np.float64,copy=True)
    points=np.asarray(corners,dtype=np.float64)
    if h.shape!=(3,3) or points.ndim!=2 or points.shape[1]!=2 or len(points)<3 or not np.isfinite(h).all() or not np.isfinite(points).all():
        raise ValueError('PERSPECTIVE_GEOMETRY_INVALID')
    if abs(np.linalg.det(h))<=1e-10 or np.linalg.cond(h)>1e8:
        raise ValueError('PERSPECTIVE_DEGENERATE')
    denominator=points@h[2,:2]+h[2,2]
    if not (np.all(denominator>1e-10) or np.all(denominator < -1e-10)):
        raise ValueError('PERSPECTIVE_HORIZON')
    h.flags.writeable=False
    return h


def manual_perspective(corners, width:int, height:int, *, max_crop_fraction:float, max_pixels:int):
    if any(type(v) is not int or v<1 for v in (width,height,max_pixels)) or width*height>200000000:
        raise ValueError('PERSPECTIVE_LIMIT_INVALID')
    if type(max_crop_fraction) not in (int,float) or not 0<=max_crop_fraction<=1:
        raise ValueError('PERSPECTIVE_LIMIT_INVALID')
    quad=np.asarray(corners,dtype=np.float64)
    if quad.shape!=(4,2) or not np.isfinite(quad).all() or np.any(quad<0) or np.any(quad>1):
        raise ValueError('PERSPECTIVE_CORNERS_INVALID')
    quad=quad*np.array([width,height],dtype=np.float64)
    edges=np.roll(quad,-1,axis=0)-quad
    turns=edges[:,0]*np.roll(edges[:,1],-1)-edges[:,1]*np.roll(edges[:,0],-1)
    if np.any(turns<=0):raise ValueError('PERSPECTIVE_CORNERS_INVALID')
    area=.5*float(np.sum(quad[:,0]*np.roll(quad[:,1],-1)-quad[:,1]*np.roll(quad[:,0],-1)))
    crop=max(0.,1-area/(width*height))
    if crop>max_crop_fraction:raise ValueError('PERSPECTIVE_CROP_LIMIT')
    lengths=np.linalg.norm(edges,axis=1)
    out_w=max(1,round(float((lengths[0]+lengths[2])/2)))
    out_h=max(1,round(float((lengths[1]+lengths[3])/2)))
    if out_w*out_h>min(max_pixels,200000000):raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    target=((0.,0.),(float(out_w),0.),(float(out_w),float(out_h)),(0.,float(out_h)))
    rows=[];rhs=[]
    for (x,y),(u,v) in zip(quad,target):
        rows.extend(([x,y,1,0,0,0,-u*x,-u*y],[0,0,0,x,y,1,-v*x,-v*y]))
        rhs.extend((u,v))
    try:h=np.append(np.linalg.solve(np.asarray(rows),np.asarray(rhs)),1.).reshape(3,3)
    except np.linalg.LinAlgError as error:raise ValueError('PERSPECTIVE_DEGENERATE') from error
    h=checked_homography(h,quad)
    return PerspectiveGeometry(h,out_w,out_h,crop)


@dataclass(frozen=True)
class PerspectiveTile:
    x:int
    y:int
    rgb:np.ndarray
    alpha:np.ndarray


def warp_perspective_tiles(rgb:np.ndarray,alpha:np.ndarray,geometry:PerspectiveGeometry,*,
                           tile:int=512,check_active:Callable[[],None]=lambda:None)->Iterator[PerspectiveTile]:
    """Bilinear premultiplied linear RGB; bounded per-tile allocations.

    Caller owns transfer conversion and scratch reservations. Colors under zero
    alpha cannot bleed into visible pixels. No clamp of linear HDR channels.
    """
    if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32 or rgb.ndim!=3 or rgb.shape[2]!=3 or
        not isinstance(alpha,np.ndarray) or alpha.dtype!=np.float32 or alpha.shape!=rgb.shape[:2]):
        raise ValueError('PERSPECTIVE_IMAGE_INVALID')
    if type(tile) is not int or not 1<=tile<=2048:raise ValueError('PERSPECTIVE_TILE_INVALID')
    sh,sw=alpha.shape
    if not sh or not sw or sh*sw>200000000:raise ValueError('PERSPECTIVE_IMAGE_INVALID')
    if any(type(v) is not int or v<1 for v in (geometry.width,geometry.height)) or geometry.width*geometry.height>200000000:
        raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    # Scan bounded input slices before producing any output.
    for y in range(0,sh,tile):
        for x in range(0,sw,tile):
            check_active()
            color_block=rgb[y:y+tile,x:x+tile]
            alpha_block=alpha[y:y+tile,x:x+tile]
            if not np.isfinite(color_block).all() or not np.isfinite(alpha_block).all() or np.any(alpha_block<0) or np.any(alpha_block>1):
                raise ValueError('PERSPECTIVE_IMAGE_INVALID')
    inverse=np.linalg.inv(checked_homography(geometry.source_to_output,[(0,0),(sw,0),(sw,sh),(0,sh)]))
    for y in range(0,geometry.height,tile):
        for x in range(0,geometry.width,tile):
            check_active()
            th,tw=min(tile,geometry.height-y),min(tile,geometry.width-x)
            xx=np.arange(x,x+tw,dtype=np.float64)[None,:]+.5
            yy=np.arange(y,y+th,dtype=np.float64)[:,None]+.5
            den=inverse[2,0]*xx+inverse[2,1]*yy+inverse[2,2]
            safe=np.abs(den)>1e-10
            divisor=np.where(safe,den,1.)
            sx=(inverse[0,0]*xx+inverse[0,1]*yy+inverse[0,2])/divisor-.5
            sy=(inverse[1,0]*xx+inverse[1,1]*yy+inverse[1,2])/divisor-.5
            valid=safe&(sx>=0)&(sx<=sw-1)&(sy>=0)&(sy<=sh-1)
            sx=np.clip(sx,0,sw-1);sy=np.clip(sy,0,sh-1)
            ix=np.floor(sx).astype(np.int64);iy=np.floor(sy).astype(np.int64)
            fx=sx-ix;fy=sy-iy
            colors=np.zeros((th,tw,3),np.float64);opacity=np.zeros((th,tw),np.float64)
            for dy in (0,1):
                for dx in (0,1):
                    weight=(fx if dx else 1-fx)*(fy if dy else 1-fy)*valid
                    px=np.minimum(ix+dx,sw-1);py=np.minimum(iy+dy,sh-1)
                    weighted_alpha=weight*alpha[py,px]
                    opacity+=weighted_alpha;colors+=rgb[py,px]*weighted_alpha[...,None]
            colors=np.divide(colors,opacity[...,None],out=np.zeros_like(colors),where=opacity[...,None]>1e-6).astype(np.float32)
            opacity=opacity.astype(np.float32)
            colors.flags.writeable=False;opacity.flags.writeable=False
            check_active()
            yield PerspectiveTile(x,y,colors,opacity)


def _line_family(segments):
    """Return owned normalized segments and the least-squares vanishing point."""
    points=np.array(segments,dtype=np.float64,copy=True)
    if points.ndim!=3 or points.shape[1:]!=(2,2) or not 2<=len(points)<=10000 or not np.isfinite(points).all():
        raise ValueError('PERSPECTIVE_LINES_INVALID')
    starts=np.column_stack((points[:,0],np.ones(len(points))))
    ends=np.column_stack((points[:,1],np.ones(len(points))))
    lines=np.cross(starts,ends)
    lengths=np.linalg.norm(lines[:,:2],axis=1)
    if np.any(lengths<=1e-10):raise ValueError('PERSPECTIVE_LINES_INVALID')
    lines/=lengths[:,None]
    # Two independent rows have an exact homogeneous nullspace. A zero row
    # exposes the third singular value without treating a valid pair as rank 1.
    if len(lines)==2:lines=np.vstack((lines,np.zeros(3)))
    _,singular,right=np.linalg.svd(lines,full_matrices=False)
    if singular[1]<=1e-10 or (singular[2]>0 and singular[1]/singular[2]<10):
        raise ValueError('PERSPECTIVE_LINE_FAMILY_DEGENERATE')
    return points,right[-1]


def _rectified_direction(points,matrix,axis):
    homogeneous=np.concatenate((points,np.ones((*points.shape[:2],1))),axis=2)
    mapped=homogeneous@matrix.T
    denominator=mapped[:,:,2]
    if np.any(np.abs(denominator)<=1e-10) or np.any(denominator[:,0]*denominator[:,1]<=0):
        raise ValueError('PERSPECTIVE_HORIZON')
    mapped=mapped[:,:,:2]/denominator[:,:,None]
    directions=mapped[:,1]-mapped[:,0]
    lengths=np.linalg.norm(directions,axis=1)
    if np.any(lengths<=1e-10):raise ValueError('PERSPECTIVE_LINE_FAMILY_DEGENERATE')
    directions/=lengths[:,None]
    signs=np.where(directions[:,axis]<0,-1.,1.)
    directions*=signs[:,None]
    direction=np.mean(directions,axis=0)
    length=np.linalg.norm(direction)
    if length<=1e-10:raise ValueError('PERSPECTIVE_LINE_FAMILY_DEGENERATE')
    return direction/length


def perspective_coverage(geometry:PerspectiveGeometry,source_width:int,source_height:int,*,tile:int=512,
                         check_active:Callable[[],None]=lambda:None)->float:
    """Count uncovered output centers without allocating the output image."""
    if type(tile) is not int or not 1<=tile<=2048:raise ValueError('PERSPECTIVE_TILE_INVALID')
    if any(type(v) is not int or v<1 for v in (source_width,source_height,geometry.width,geometry.height)) or source_width*source_height>200000000 or geometry.width*geometry.height>200000000:
        raise ValueError('PERSPECTIVE_LIMIT_INVALID')
    inverse=np.linalg.inv(checked_homography(geometry.source_to_output,
        [(0,0),(source_width,0),(source_width,source_height),(0,source_height)]))
    covered=0
    for y in range(0,geometry.height,tile):
        for x in range(0,geometry.width,tile):
            check_active()
            xx=np.arange(x,min(x+tile,geometry.width),dtype=np.float64)[None,:]+.5
            yy=np.arange(y,min(y+tile,geometry.height),dtype=np.float64)[:,None]+.5
            denominator=inverse[2,0]*xx+inverse[2,1]*yy+inverse[2,2]
            safe=np.abs(denominator)>1e-10
            divisor=np.where(safe,denominator,1.)
            sx=(inverse[0,0]*xx+inverse[0,1]*yy+inverse[0,2])/divisor-.5
            sy=(inverse[1,0]*xx+inverse[1,1]*yy+inverse[1,2])/divisor-.5
            covered+=int(np.count_nonzero(safe&(sx>=0)&(sx<=source_width-1)&(sy>=0)&(sy<=source_height-1)))
    return 1-covered/(geometry.width*geometry.height)


def auto_perspective(horizontal,vertical,width:int,height:int,*,max_crop_fraction:float,max_pixels:int,
                     tile:int=512,check_active:Callable[[],None]=lambda:None)->PerspectiveGeometry:
    """Rectify pixel-edge line endpoints; preview/review is required by caller.

    No automatic crop, inferred camera model or silent fallback is performed.
    The uncovered fraction includes geometric support only, not source alpha.
    """
    if any(type(v) is not int or v<1 for v in (width,height,max_pixels)) or width*height>200000000:
        raise ValueError('PERSPECTIVE_LIMIT_INVALID')
    if type(max_crop_fraction) not in (int,float) or not 0<=max_crop_fraction<=1:
        raise ValueError('PERSPECTIVE_LIMIT_INVALID')
    check_active()
    h_lines,vh=_line_family(horizontal)
    v_lines,vv=_line_family(vertical)
    for lines in (h_lines,v_lines):
        if np.any(lines<0) or np.any(lines[:,:,0]>width) or np.any(lines[:,:,1]>height):
            raise ValueError('PERSPECTIVE_LINES_INVALID')
    horizon=np.cross(vh,vv)
    if not np.isfinite(horizon).all() or abs(horizon[2])<=1e-8:
        raise ValueError('PERSPECTIVE_HORIZON_UNSUPPORTED')
    horizon/=horizon[2]
    projective=np.array([[1.,0.,0.],[0.,1.,0.],[horizon[0],horizon[1],1.]])
    corners=np.array([[0.,0.],[width,0.],[width,height],[0.,height]])
    checked_homography(projective,corners)
    horizontal_direction=_rectified_direction(h_lines,projective,0)
    vertical_direction=_rectified_direction(v_lines,projective,1)
    directions=np.column_stack((horizontal_direction,vertical_direction))
    if abs(np.linalg.det(directions))<=1e-10 or np.linalg.cond(directions)>1e8:
        raise ValueError('PERSPECTIVE_LINE_FAMILY_DEGENERATE')
    affine=np.eye(3);affine[:2,:2]=np.linalg.inv(directions)
    matrix=checked_homography(affine@projective,corners)
    warped=np.column_stack((corners,np.ones(4)))@matrix.T
    warped=warped[:,:2]/warped[:,2,None]
    # SVD of exactly parallel families can leave machine-epsilon offsets.
    # Stabilize integer bounds only within floating-point roundoff, avoiding
    # an extra transparent border for the identity mapping.
    nearest=np.rint(warped)
    tolerance=64*np.finfo(np.float64).eps*np.maximum(1.,np.abs(warped))
    warped=np.where(np.abs(warped-nearest)<=tolerance,nearest,warped)
    minimum=np.floor(warped.min(axis=0));maximum=np.ceil(warped.max(axis=0))
    dimensions=maximum-minimum
    # Check in float before conversion: adversarial horizons must not overflow.
    if not np.isfinite(dimensions).all() or np.any(dimensions<1) or np.any(dimensions>max_pixels) or dimensions[0]*dimensions[1]>min(max_pixels,200000000):
        raise ValueError('PERSPECTIVE_PIXEL_LIMIT')
    translation=np.eye(3);translation[:2,2]=-minimum
    matrix=checked_homography(translation@matrix,corners)
    geometry=PerspectiveGeometry(matrix,int(dimensions[0]),int(dimensions[1]),0.)
    uncovered=perspective_coverage(geometry,width,height,tile=tile,check_active=check_active)
    if uncovered>max_crop_fraction:raise ValueError('PERSPECTIVE_CROP_LIMIT')
    check_active()
    return PerspectiveGeometry(matrix,geometry.width,geometry.height,uncovered)

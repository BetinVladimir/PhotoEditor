"""Normative V25 tiled inverse lens map. Qualification is a caller prerequisite."""
from dataclasses import dataclass
from math import isfinite
from typing import Callable, Iterator
import numpy as np
from photo_worker.operations.optical.contracts import LensProfile
from .warp import validate_shape

@dataclass(frozen=True)
class LensTile:
    x: int
    y: int
    green: np.ndarray
    red: np.ndarray
    blue: np.ndarray
    gain: np.ndarray


def match_lens_metadata(profile: LensProfile, camera: str, lens: str,
                        focal_mm: float, aperture: float) -> None:
    if (camera != profile.camera_model or lens != profile.lens_model or
            not isfinite(focal_mm) or not isfinite(aperture) or
            abs(focal_mm-profile.focal_mm)>1e-6 or
            abs(aperture-profile.aperture)>1e-6):
        raise ValueError('LENS_PROFILE_MISMATCH')


def _green_map(profile: LensProfile, shape: tuple[int,int], strength: float,
               px: np.ndarray, py: np.ndarray):
    height,width=shape;scale=max(width,height);cx,cy=profile.cx*width,profile.cy*height
    x,y=(px-cx)/scale,(py-cy)/scale;r2=x*x+y*y
    k1,k2,k3=profile.k;p1,p2=profile.p
    radial=1+k1*r2+k2*r2*r2+k3*r2*r2*r2
    xd=x*radial+2*p1*x*y+p2*(r2+2*x*x)
    yd=y*radial+p1*(r2+2*y*y)+2*p2*x*y
    return px+strength*(cx+xd*scale-px),py+strength*(cy+yd*scale-py)


def lens_points(profile: LensProfile, shape: tuple[int,int], strength: float,
                points: np.ndarray) -> np.ndarray:
    validate_shape(shape)
    points=np.asarray(points,dtype=np.float64)
    if not isfinite(strength) or not 0<=strength<=1 or points.ndim!=2 or points.shape[1]!=2 or not np.isfinite(points).all():
        raise ValueError('LENS_PARAMS_INVALID')
    x,y=_green_map(profile,shape,strength,points[:,0],points[:,1])
    return np.stack((x,y),axis=-1)


def _jacobians(profile,shape,strength,points):
    h=1e-3
    dx=(lens_points(profile,shape,strength,points+[h,0])-lens_points(profile,shape,strength,points-[h,0]))/(2*h)
    dy=(lens_points(profile,shape,strength,points+[0,h])-lens_points(profile,shape,strength,points-[0,h]))/(2*h)
    return np.stack((dx,dy),axis=-1)


def validate_lens_geometry(profile: LensProfile, shape: tuple[int,int], strength: float) -> None:
    validate_shape(shape)
    height,width=shape
    # Include all canvas-bound corners, not only interior pixel centers.
    x,y=np.meshgrid(np.linspace(0,width,33),np.linspace(0,height,33))
    jac=_jacobians(profile,shape,strength,np.stack((x.ravel(),y.ravel()),axis=-1))
    det=jac[:,0,0]*jac[:,1,1]-jac[:,0,1]*jac[:,1,0]
    if not np.isfinite(det).all() or np.any(det<=0):
        raise ValueError('LENS_NON_INVERTIBLE')


def lens_forward_point(profile: LensProfile, shape: tuple[int,int], strength: float,
                       source_point: tuple[float,float]) -> tuple[float,float] | None:
    """Invert green geometry by bounded Newton iteration; failure is unmeasurable."""
    try:
        validate_lens_geometry(profile,shape,strength)
    except ValueError as error:
        if str(error)=='LENS_NON_INVERTIBLE':return None
        raise
    target=np.asarray(source_point,dtype=np.float64)
    if target.shape!=(2,) or not np.isfinite(target).all():return None
    height,width=shape
    if not (.5<=target[0]<=width-.5 and .5<=target[1]<=height-.5):return None
    point=target.copy()
    for step in range(21):
        residual=lens_points(profile,shape,strength,point[None,:])[0]-target
        if np.linalg.norm(residual)<=1e-4:
            return (float(point[0]),float(point[1])) if .5<=point[0]<=width-.5 and .5<=point[1]<=height-.5 else None
        if step==20:break
        jac=_jacobians(profile,shape,strength,point[None,:])[0]
        det=jac[0,0]*jac[1,1]-jac[0,1]*jac[1,0]
        if not isfinite(det) or abs(det)<1e-10:return None
        point-=np.linalg.solve(jac,residual)
        if not np.isfinite(point).all():return None
    return None


def lens_map_tiles(profile: LensProfile, shape: tuple[int,int], strength: float,
                   *, tile: int=512, check_active: Callable[[],None]=lambda:None) -> Iterator[LensTile]:
    validate_shape(shape)
    if not isfinite(strength) or not 0<=strength<=1 or type(tile) is not int or not 1<=tile<=2048:
        raise ValueError('LENS_PARAMS_INVALID')
    validate_lens_geometry(profile,shape,strength)
    height,width=shape; scale=max(width,height)
    cx,cy=profile.cx*width,profile.cy*height
    v1,v2,v3=profile.vignette
    for top in range(0,height,tile):
        for left in range(0,width,tile):
            check_active()
            px,py=np.meshgrid(np.arange(left,min(width,left+tile),dtype=np.float64)+.5,
                              np.arange(top,min(height,top+tile),dtype=np.float64)+.5)
            x,y=(px-cx)/scale,(py-cy)/scale;r2=x*x+y*y
            gx,gy=_green_map(profile,shape,strength,px,py)
            def channel(ca):
                factor=1+strength*(ca-1)
                return np.stack((cx+(gx-cx)*factor,cy+(gy-cy)*factor),axis=-1).astype(np.float32)
            gain=np.power(1/np.maximum(.25,1+v1*r2+v2*r2*r2+v3*r2*r2*r2),strength).astype(np.float32)
            yield LensTile(left,top,np.stack((gx,gy),axis=-1).astype(np.float32),
                           channel(profile.ca_red_scale),channel(profile.ca_blue_scale),gain)


@dataclass(frozen=True)
class LensImageTile:
    x: int
    y: int
    rgb: np.ndarray
    alpha: np.ndarray
    uncovered: bool


def _sample_channel(rgb, alpha, coordinates, channel):
    """Gather premultiplied neighbors without allocating a full image copy."""
    height,width=alpha.shape
    u=coordinates[...,0].astype(np.float64)-.5
    v=coordinates[...,1].astype(np.float64)-.5
    covered=(u>=0)&(u<=width-1)&(v>=0)&(v<=height-1)
    u=np.clip(u,0,width-1);v=np.clip(v,0,height-1)
    x0=np.floor(u).astype(np.int64);y0=np.floor(v).astype(np.int64)
    x1=np.minimum(x0+1,width-1);y1=np.minimum(y0+1,height-1)
    dx=u-x0;dy=v-y0
    opacity=np.zeros(u.shape,dtype=np.float64)
    premult=np.zeros(u.shape,dtype=np.float64)
    for x,y,weight in ((x0,y0,(1-dx)*(1-dy)),(x1,y0,dx*(1-dy)),
                       (x0,y1,(1-dx)*dy),(x1,y1,dx*dy)):
        a=alpha[y,x].astype(np.float64)
        opacity+=a*weight
        premult+=rgb[y,x,channel].astype(np.float64)*a*weight
    straight=np.zeros(u.shape,dtype=np.float64)
    np.divide(premult,opacity,out=straight,where=opacity>1e-6)
    return straight,np.where(covered,opacity,0),covered


def resample_lens_tiles(profile: LensProfile, rgb: np.ndarray, alpha: np.ndarray,
                        strength: float, *, tile: int=512,
                        check_active: Callable[[],None]=lambda:None) -> Iterator[LensImageTile]:
    """Linear HDR, straight-alpha output; CA uses conservative channel opacity.

    Callers supply an ndarray or disk-backed ndarray. Intermediate allocations
    are tile-sized. Zero strength preserves even transparent hidden RGB exactly.
    """
    if (not isinstance(rgb,np.ndarray) or not isinstance(alpha,np.ndarray) or
            rgb.dtype!=np.float32 or alpha.dtype!=np.float32 or
            rgb.ndim!=3 or rgb.shape[-1]!=3 or alpha.shape!=rgb.shape[:2]):
        raise ValueError('LENS_IMAGE_INVALID')
    validate_shape(alpha.shape)
    if not isfinite(strength) or not 0<=strength<=1 or type(tile) is not int or not 1<=tile<=2048:
        raise ValueError('LENS_PARAMS_INVALID')
    height,width=alpha.shape
    # Validate in bounded slices, including inputs outside sampled coverage.
    for top in range(0,height,tile):
        check_active()
        for left in range(0,width,tile):
            colors=rgb[top:top+tile,left:left+tile];opacity=alpha[top:top+tile,left:left+tile]
            if not np.isfinite(colors).all() or not np.isfinite(opacity).all() or np.any((opacity<0)|(opacity>1)):
                raise ValueError('LENS_IMAGE_INVALID')
    if strength==0:
        for top in range(0,height,tile):
            for left in range(0,width,tile):
                check_active()
                yield LensImageTile(left,top,rgb[top:top+tile,left:left+tile].copy(),
                                    alpha[top:top+tile,left:left+tile].copy(),False)
        return
    for mapping in lens_map_tiles(profile,alpha.shape,strength,tile=tile,check_active=check_active):
        channels=[];opacities=[];coverage=[]
        for index,coordinates in enumerate((mapping.red,mapping.green,mapping.blue)):
            color,opacity,covered=_sample_channel(rgb,alpha,coordinates,index)
            channels.append(color);opacities.append(opacity);coverage.append(covered)
        output_alpha=np.minimum.reduce(opacities).astype(np.float32)
        output=np.stack(channels,axis=-1)*mapping.gain[...,None]
        output[output_alpha<=1e-6]=0
        output=output.astype(np.float32)
        if not np.isfinite(output).all():raise ValueError('LENS_IMAGE_INVALID')
        yield LensImageTile(mapping.x,mapping.y,output,output_alpha,
                            bool(np.any(~np.logical_and.reduce(coverage))))


@dataclass(frozen=True)
class LensMaskTile:
    x: int
    y: int
    values: np.ndarray


def resample_lens_mask_tiles(profile: LensProfile, mask: np.ndarray,
                             strength: float, *, tile: int=512,
                             check_active: Callable[[],None]=lambda:None) -> Iterator[LensMaskTile]:
    """Probability masks follow green geometry, without CA or vignette gain."""
    if not isinstance(mask,np.ndarray) or mask.ndim!=2 or mask.dtype!=np.float32:
        raise ValueError('LENS_MASK_INVALID')
    validate_shape(mask.shape)
    if not isfinite(strength) or not 0<=strength<=1 or type(tile) is not int or not 1<=tile<=2048:
        raise ValueError('LENS_PARAMS_INVALID')
    height,width=mask.shape
    for top in range(0,height,tile):
        for left in range(0,width,tile):
            check_active();block=mask[top:top+tile,left:left+tile]
            if not np.isfinite(block).all() or np.any((block<0)|(block>1)):
                raise ValueError('LENS_MASK_INVALID')
    for mapping in lens_map_tiles(profile,mask.shape,strength,tile=tile,check_active=check_active):
        if strength==0:
            h,w=mapping.gain.shape
            values=mask[mapping.y:mapping.y+h,mapping.x:mapping.x+w].copy()
        else:
            u=mapping.green[...,0].astype(np.float64)-.5
            v=mapping.green[...,1].astype(np.float64)-.5
            covered=(u>=0)&(u<=width-1)&(v>=0)&(v<=height-1)
            u=np.clip(u,0,width-1);v=np.clip(v,0,height-1)
            x0=np.floor(u).astype(np.int64);y0=np.floor(v).astype(np.int64)
            x1=np.minimum(x0+1,width-1);y1=np.minimum(y0+1,height-1)
            dx=u-x0;dy=v-y0
            values=(mask[y0,x0]*(1-dx)*(1-dy)+mask[y0,x1]*dx*(1-dy)+
                    mask[y1,x0]*(1-dx)*dy+mask[y1,x1]*dx*dy)
            values=np.where(covered,values,0).astype(np.float32)
        yield LensMaskTile(mapping.x,mapping.y,values)

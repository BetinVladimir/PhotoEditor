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


def identity_inverse_field(width:int,height:int,*,check_active=lambda:None)->np.ndarray:
    if type(width) is not int or type(height) is not int or min(width,height)<1 or width*height>40000000:
        raise ValueError('BODY_FIELD_LAYOUT_INVALID')
    check_active()
    field=np.empty((height,width,2),np.float32)
    rows=max(1,min(256,65536//width))
    xs=np.arange(width,dtype=np.float32)
    for y in range(0,height,rows):
        check_active();stop=min(y+rows,height)
        field[y:stop,:,0]=xs
        field[y:stop,:,1]=np.arange(y,stop,dtype=np.float32)[:,None]
    check_active()
    return field


def solve_inverse_field(width:int,height:int,sets:list[tuple[np.ndarray,np.ndarray,np.ndarray]],person:np.ndarray,*,check_active=lambda:None)->np.ndarray:
    field=identity_inverse_field(width,height,check_active=check_active)
    coverage=np.zeros((height,width),np.uint8)
    for points,values,roi in sets:
        check_active()
        x0,y0,x1,y1=np.floor(roi).astype(int);x1=min(width-1,int(np.ceil(roi[2])));y1=min(height-1,int(np.ceil(roi[3])))
        if x1<x0 or y1<y0:continue
        sigma=max(.2*max(x1-x0+1,y1-y0+1),1.);delta=points[:,None,:]-points[None,:,:]
        kernel=np.exp(-np.sum(delta*delta,axis=2)/(2*sigma*sigma));weights=np.linalg.solve(kernel+np.eye(len(points))*1e-4,values)
        # RBF query tensors scale with anchors as well as pixels. Bound both.
        tile_pixels=max(1,65536//max(1,len(points)))
        tile_width=min(x1-x0+1,tile_pixels)
        tile_rows=max(1,min(256,tile_pixels//tile_width))
        for y in range(y0,y1+1,tile_rows):
            for x in range(x0,x1+1,tile_width):
                check_active();stop_y=min(y+tile_rows,y1+1);stop_x=min(x+tile_width,x1+1)
                ry,rx=np.mgrid[y:stop_y,x:stop_x];query=np.stack((rx,ry),axis=-1);d=query[...,None,:]-points
                moved=np.exp(-np.sum(d*d,axis=-1)/(2*sigma*sigma))@weights
                gate=np.clip(person[y:stop_y,x:stop_x,...],0,1)[...,None]
                active=np.linalg.norm(moved,axis=-1)>.05
                prior=coverage[y:stop_y,x:stop_x]
                if np.any(active&(prior>0)):raise ValueError('AMBIGUOUS_OVERLAP')
                field[y:stop_y,x:stop_x]+=np.asarray(moved*gate,np.float32)
                coverage[y:stop_y,x:stop_x]+=active
    check_active()
    return field


def validate_field(field:np.ndarray,max_displacement:float,person:np.ndarray,*,check_active=lambda:None)->WarpValidation:
    check_active()
    h,w,_=field.shape
    if h<2 or w<2 or field.shape!=(h,w,2) or person.shape!=(h,w):
        raise ValueError('BODY_FIELD_LAYOUT_INVALID')
    maximum=0.;minimum=float('inf');center=0.
    # One-pixel halo preserves NumPy central/edge differences at tile seams.
    for y in range(0,h,256):
        for x in range(0,w,256):
            check_active();ey=min(y+256,h);ex=min(x+256,w)
            yy,xx=np.mgrid[y:ey,x:ex];part=field[y:ey,x:ex]
            if not np.isfinite(part).all():raise ValueError('BODY_FIELD_NONFINITE')
            distance=np.hypot(part[...,0]-xx,part[...,1]-yy)
            maximum=max(maximum,float(np.max(distance)))
            protected=person[y:ey,x:ex]<.01
            if np.any(protected):center=max(center,float(np.max(distance[protected])))
            hy=max(0,y-1);hx=max(0,x-1);hey=min(h,ey+1);hex_=min(w,ex+1)
            local=field[hy:hey,hx:hex_]
            gx_x=np.gradient(local[...,0],axis=1);gx_y=np.gradient(local[...,0],axis=0)
            gy_x=np.gradient(local[...,1],axis=1);gy_y=np.gradient(local[...,1],axis=0)
            det=(gx_x*gy_y-gx_y*gy_x)[y-hy:ey-hy,x-hx:ex-hx]
            ys=np.arange(y,ey);xs=np.arange(x,ex)
            sy=(ys%4==0)|(ys==h-1);sx=(xs%4==0)|(xs==w-1)
            if np.any(sy) and np.any(sx):minimum=min(minimum,float(np.min(det[np.ix_(sy,sx)])))
    check_active()
    protected_limit=.1*max(h,w)/1024
    return WarpValidation(maximum<=max_displacement and minimum>=.5 and center<=protected_limit,maximum,minimum,center)


def bilinear(source:np.ndarray,field:np.ndarray)->np.ndarray:
    h,w=source.shape[:2];sx=np.clip(field[...,0],0,w-1);sy=np.clip(field[...,1],0,h-1)
    x0=np.floor(sx).astype(np.int64);y0=np.floor(sy).astype(np.int64);x1=np.minimum(x0+1,w-1);y1=np.minimum(y0+1,h-1)
    fx=sx-x0;fy=sy-y0
    if source.ndim==3:fx=fx[...,None];fy=fy[...,None]
    return ((source[y0,x0]*(1-fx)+source[y0,x1]*fx)*(1-fy)+(source[y1,x0]*(1-fx)+source[y1,x1]*fx)*fy)


def warp_rgb_into(rgb, alpha, field, output, *, check_active=lambda: None):
    """Linear-light body resampling; hidden RGB never contributes through alpha.

    Tile the inverse sampler to bound temporary index/interpolation arrays.
    Alpha output remains the caller's existing uint16 bilinear contract.
    """
    from photo_worker.algorithms.color import to_linear, to_encoded
    check_active()
    h,w=rgb.shape[:2]
    if (rgb.dtype!=np.float32 or rgb.shape!=(h,w,3) or output.shape!=rgb.shape
            or output.dtype!=np.float32 or field.shape!=(h,w,2) or field.dtype!=np.float32
            or alpha is not None and (alpha.shape!=(h,w) or alpha.dtype!=np.uint16)):
        raise ValueError('BODY_RGBA_LAYOUT_INVALID')
    linear=to_linear(np.asarray(rgb))
    translucent=alpha is not None and np.any(alpha!=65535)
    opacity=None
    if translucent:
        opacity=np.asarray(alpha,np.float32)/65535
        linear*=opacity[...,None]
    rows=max(1,min(256,65536//w))
    for y in range(0,h,rows):
        check_active();sl=slice(y,min(y+rows,h));tile=field[sl]
        if not np.isfinite(tile).all():raise ValueError('BODY_RGBA_FIELD_INVALID')
        warped=np.asarray(bilinear(linear,tile),np.float32)
        if opacity is not None:
            coverage=np.clip(bilinear(opacity,tile),0,1)
            decoded=np.zeros_like(warped)
            np.divide(warped,coverage[...,None],out=decoded,where=coverage[...,None]>1e-6)
            warped=decoded
        output[sl]=to_encoded(warped)
        # Exact identity coordinates represent protected/unmodified pixels.
        # Preserve encoded values rather than round-tripping through gamma or
        # unpremultiplication (including invisible RGB at zero alpha).
        stationary=(tile[...,0]==np.arange(w,dtype=np.float32)[None,:]) & (
            tile[...,1]==np.arange(y,sl.stop,dtype=np.float32)[:,None])
        np.copyto(output[sl],rgb[sl],where=stationary[...,None])
    check_active()


def warp_alpha_into(alpha, field, output, *, check_active=lambda: None):
    """Preserve uint16 bilinear alpha math with bounded temporary arrays."""
    check_active()
    if (alpha.ndim!=2 or alpha.dtype!=np.uint16 or output.shape!=alpha.shape
            or output.dtype!=np.uint16 or field.shape!=(*alpha.shape,2)
            or field.dtype!=np.float32):
        raise ValueError('BODY_ALPHA_LAYOUT_INVALID')
    h,w=alpha.shape
    source=np.asarray(alpha,np.float32)
    rows=max(1,min(256,65536//w))
    for y in range(0,h,rows):
        check_active();sl=slice(y,min(y+rows,h));tile=field[sl]
        if not np.isfinite(tile).all():raise ValueError('BODY_ALPHA_FIELD_INVALID')
        output[sl]=np.rint(np.clip(bilinear(source,tile),0,65535)).astype(np.uint16)
    check_active()

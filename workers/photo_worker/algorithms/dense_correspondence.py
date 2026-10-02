"""Source correspondence for explicit legacy/new dense-field conventions."""
import math
import numpy as np
from photo_worker.contracts.images import DenseWarpTransform


def dense_source_point(step:DenseWarpTransform,field:np.ndarray,
                       point:tuple[float,float])->tuple[float,float]|None:
    """Map an output pixel-center point to source centers, or unmeasurable.

    Public point coordinates use x+.5,y+.5. Historical stored integer-index
    fields are translated to centers; marked fields already contain centers.
    No homography approximation, clamping or invented source coverage is used.
    """
    height,width=step.to_canvas.height,step.to_canvas.width
    if not isinstance(field,np.ndarray) or field.dtype!=np.float32 or field.shape!=(height,width,2):
        raise ValueError('DENSE_FIELD_INVALID')
    if len(point)!=2 or not all(math.isfinite(v) for v in point):return None
    px,py=point;x,y=px-.5,py-.5
    region=step.valid_region
    if not (0<=x<=width-1 and 0<=y<=height-1 and
            region.x<=x<=region.x+region.width-1 and region.y<=y<=region.y+region.height-1):return None
    x0,y0=math.floor(x),math.floor(y);x1,y1=min(x0+1,width-1),min(y0+1,height-1)
    fx,fy=x-x0,y-y0
    corners=np.array([field[y0,x0],field[y0,x1],field[y1,x0],field[y1,x1]],np.float64)
    if not np.isfinite(corners).all():raise ValueError('DENSE_FIELD_INVALID')
    mapped=corners.T@np.array([(1-fx)*(1-fy),fx*(1-fy),(1-fx)*fy,fx*fy])
    if step.pixel_centers is None:mapped+=.5
    sw,sh=step.from_canvas.width,step.from_canvas.height
    if not (.5<=mapped[0]<=sw-.5 and .5<=mapped[1]<=sh-.5):return None
    return float(mapped[0]),float(mapped[1])


def chain_source_point(chain,point,resolve_field,*,check_active=lambda:None):
    """Walk a verified transform chain backwards; resolver supplies SHA-checked fields.

    Matrix conventions match legacy integer-index geometry. Resize replicates
    border centers as the mask consumer does. Nonlinear fields remain nonlinear.
    A point with no covered correspondence returns None; malformed transforms
    or field evidence raise rather than being replaced with identity.
    """
    from photo_worker.contracts.images import MatrixTransform,CropTransform,ResizeTransform
    chain.verify_chain()
    if len(point)!=2 or not all(math.isfinite(value) for value in point):return None
    current=(float(point[0]),float(point[1]))
    for step in reversed(chain.steps):
        check_active();px,py=current
        region=step.valid_region;x,y=px-.5,py-.5
        if not (0<=x<=step.to_canvas.width-1 and 0<=y<=step.to_canvas.height-1 and
                region.x<=x<=region.x+region.width-1 and region.y<=y<=region.y+region.height-1):return None
        if isinstance(step,DenseWarpTransform):
            current=dense_source_point(step,resolve_field(step.inverse_field),current)
            if current is None:return None
            continue
        if isinstance(step,CropTransform):
            current=(px+step.crop_rect.x,py+step.crop_rect.y)
        elif isinstance(step,ResizeTransform):
            current=(min(step.from_canvas.width-.5,max(.5,px*step.from_canvas.width/step.to_canvas.width)),
                     min(step.from_canvas.height-.5,max(.5,py*step.from_canvas.height/step.to_canvas.height)))
        elif isinstance(step,MatrixTransform):
            inverse=np.linalg.inv(np.asarray(step.forward_matrix,np.float64).reshape(3,3))
            mapped=inverse@np.array([x,y,1.])
            if not np.isfinite(mapped).all() or abs(mapped[2])<=1e-12:return None
            current=(float(mapped[0]/mapped[2]+.5),float(mapped[1]/mapped[2]+.5))
        else:raise ValueError('UNSUPPORTED_TRANSFORM')
        if not (.5<=current[0]<=step.from_canvas.width-.5 and .5<=current[1]<=step.from_canvas.height-.5):return None
    return current

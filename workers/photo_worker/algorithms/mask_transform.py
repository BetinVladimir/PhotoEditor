"""Exact-coordinate tiled resampling for one explicit geometry step."""
from dataclasses import dataclass
from collections.abc import Callable,Iterator
import numpy as np
from photo_worker.contracts.images import MatrixTransform,CropTransform,ResizeTransform,DenseWarpTransform,TransformStep

@dataclass(frozen=True)
class TransformedMaskTile:
    x:int
    y:int
    values:np.ndarray


def transform_tiles(source:np.ndarray,step:TransformStep,*,labels:bool=False,inverse_field:np.ndarray|None=None,
                    tile_size:int=512,check_active:Callable[[],None]=lambda:None)->Iterator[TransformedMaskTile]:
    """Bilinear probabilities; nearest uint16 labels; no color transfer function.

    Dense fields contain absolute source pixel-center coordinates(x,y), not
    displacement. Only resize replicates borders; other transforms use zero.
    Guided edge refinement is a separate stage with the target-image guide.
    """
    check_active()
    if type(tile_size) is not int or tile_size not in (256,512,1024):raise ValueError('Invalid mask tile size')
    sh,sw=step.from_canvas.height,step.from_canvas.width
    h,w=step.to_canvas.height,step.to_canvas.width
    if max(sh*sw,h*w)>200000000:raise ValueError('Mask pixel limit')
    if source.shape!=(sh,sw) or source.dtype!=(np.uint16 if labels else np.float32):raise ValueError('Invalid source mask')
    if not labels:
        for row in range(sh):
            for x in range(0,sw,16384):
                check_active();block=source[row,x:x+16384]
                if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):raise ValueError('Invalid mask probability')
    if isinstance(step,DenseWarpTransform):
        if inverse_field is None or inverse_field.shape!=(h,w,2) or inverse_field.dtype!=np.float32:
            raise ValueError('Invalid dense inverse field')
    elif inverse_field is not None:raise ValueError('Unexpected inverse field')
    inverse=np.linalg.inv(np.array(step.forward_matrix,np.float64).reshape(3,3)) if isinstance(step,MatrixTransform) else None
    for y in range(0,h,tile_size):
        for x in range(0,w,tile_size):
            check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x)
            xx=np.arange(x,x+tw,dtype=np.float64)[None,:];yy=np.arange(y,y+th,dtype=np.float64)[:,None]
            valid=np.ones((th,tw),bool)
            if isinstance(step,CropTransform):sx=xx+step.crop_rect.x;sy=yy+step.crop_rect.y
            elif isinstance(step,ResizeTransform):
                sx=(xx+.5)*sw/w-.5;sy=(yy+.5)*sh/h-.5
                sx=np.clip(sx,0,sw-1);sy=np.clip(sy,0,sh-1)
            elif isinstance(step,DenseWarpTransform):
                field=inverse_field[y:y+th,x:x+tw]
                if not np.isfinite(field).all():raise ValueError('Non-finite inverse field')
                offset=.5 if step.pixel_centers=='half_pixel' else 0.
                sx=field[...,0].astype(np.float64)-offset;sy=field[...,1].astype(np.float64)-offset
                if step.pixel_centers=='half_pixel':
                    valid &= (sx>=0)&(sx<=sw-1)&(sy>=0)&(sy<=sh-1)
            elif isinstance(step,MatrixTransform):
                denominator=inverse[2,0]*xx+inverse[2,1]*yy+inverse[2,2]
                valid &= np.abs(denominator)>1e-12
                safe=np.where(valid,denominator,1.)
                sx=(inverse[0,0]*xx+inverse[0,1]*yy+inverse[0,2])/safe
                sy=(inverse[1,0]*xx+inverse[1,1]*yy+inverse[1,2])/safe
            else:raise ValueError('Unsupported transform')
            # Saturate before integer conversion; distant points still contribute zero.
            sx=np.clip(sx,-2,sw+1);sy=np.clip(sy,-2,sh+1)
            if labels:
                ix=np.floor(sx+.5).astype(np.int64);iy=np.floor(sy+.5).astype(np.int64)
                support=(ix>=0)&(ix<sw)&(iy>=0)&(iy<sh)
                output=np.where(support,source[np.clip(iy,0,sh-1),np.clip(ix,0,sw-1)],0).astype(np.uint16)
            else:
                ix=np.floor(sx).astype(np.int64);iy=np.floor(sy).astype(np.int64)
                fx=sx-ix;fy=sy-iy;output=np.zeros((th,tw),np.float64)
                for dy in (0,1):
                    for dx in (0,1):
                        check_active();cx=ix+dx;cy=iy+dy
                        support=(cx>=0)&(cx<sw)&(cy>=0)&(cy<sh)
                        weight=(fx if dx else 1-fx)*(fy if dy else 1-fy)
                        output+=np.where(support,source[np.clip(cy,0,sh-1),np.clip(cx,0,sw-1)],0)*weight
                output=np.clip(output,0,1).astype(np.float32)
            r=step.valid_region
            valid &= (xx>=r.x)&(xx<r.x+r.width)&(yy>=r.y)&(yy<r.y+r.height)
            output[~valid]=0;output.flags.writeable=False
            yield TransformedMaskTile(x,y,output)

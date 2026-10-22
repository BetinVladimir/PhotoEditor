"""Binary provenance support, separate from probability-mask refinement.

Body interpolation uses four clamped bilinear contributors. Crop and outpaint
translations are exact. Neural upscale has nonlocal learned support: any prior
AI contribution conservatively marks the complete result, never nearest-only.
"""
import numpy as np
from photo_worker.contracts.images import CropTransform, DenseWarpTransform, MatrixTransform, ResizeTransform


def generated_support_tiles(source, step, *, operation, inverse_field=None, tile_size=512, check_active=lambda: None):
    sh, sw = step.from_canvas.height, step.from_canvas.width
    h, w = step.to_canvas.height, step.to_canvas.width
    if (source.dtype != np.float32 or source.shape != (sh,sw) or max(sh*sw,h*w)>200000000
            or type(tile_size) is not int or tile_size not in (256,512,1024)):
        raise ValueError('GENERATED_SUPPORT_SOURCE_INVALID')
    present = False
    for y in range(0,sh,256):
        check_active(); block = source[y:y+256]
        if not np.all((block==0)|(block==1)): raise ValueError('GENERATED_SUPPORT_BINARY_REQUIRED')
        present |= bool(np.any(block))
    if operation == 'image.upscale':
        if not isinstance(step, ResizeTransform) or inverse_field is not None:
            raise ValueError('GENERATED_SUPPORT_UPSCALE_STEP')
        for y in range(0,h,tile_size):
            for x in range(0,w,tile_size):
                check_active(); yield x,y,np.full((min(tile_size,h-y),min(tile_size,w-x)),float(present),np.float32)
        return
    if operation == 'derivative.resize':
        if not isinstance(step,ResizeTransform) or inverse_field is not None:
            raise ValueError('GENERATED_SUPPORT_RESIZE_STEP')
        if (w,h)==(sw,sh):
            for y in range(0,h,tile_size):
                for x in range(0,w,tile_size):
                    check_active();yield x,y,np.array(source[y:y+tile_size,x:x+tile_size],copy=True)
            return
        if (w<sw and h>sh) or (h<sh and w>sw):
            # OpenCV's mixed-axis AREA emulation is not a simple box on both
            # axes. A conservative full support envelope avoids false negatives.
            for y in range(0,h,tile_size):
                for x in range(0,w,tile_size):
                    check_active();yield x,y,np.full((min(tile_size,h-y),min(tile_size,w-x)),float(present),np.float32)
            return
        if w<sw or h<sh:
            # INTER_AREA contributor rectangles. Integer arithmetic excludes
            # zero-area boundaries; row reduction scans at most two overlapping
            # output intervals per source row, with O(source_width) workspace.
            starts=(np.arange(w,dtype=np.int64)*sw)//w
            ends=((np.arange(1,w+1,dtype=np.int64)*sw)+w-1)//w
            for y in range(h):
                check_active(); first=(y*sh)//h;last=((y+1)*sh+h-1)//h
                columns=np.any(source[first:last],axis=0)
                prefix=np.empty(sw+1,np.int64);prefix[0]=0
                np.cumsum(columns,dtype=np.int64,out=prefix[1:])
                values=(prefix[ends]-prefix[starts]>0).astype(np.float32)[None,:]
                for x in range(0,w,tile_size):
                    check_active();yield x,y,values[:,x:x+tile_size]
            return
        # INTER_LANCZOS4 uses the eight source samples around each coordinate,
        # with replicated borders. Preserve every possible nonzero contributor
        # (including tiny floating kernel tails), independently of RGB values.
        for y in range(0,h,tile_size):
            for x in range(0,w,tile_size):
                check_active();th,tw=min(tile_size,h-y),min(tile_size,w-x)
                sx=(np.arange(x,x+tw,dtype=np.float64)[None,:]+.5)*sw/w-.5
                sy=(np.arange(y,y+th,dtype=np.float64)[:,None]+.5)*sh/h-.5
                ix=np.floor(sx).astype(np.int64);iy=np.floor(sy).astype(np.int64)
                values=np.zeros((th,tw),bool)
                for dy in range(-3,5):
                    for dx in range(-3,5):
                        check_active();values|=source[np.clip(iy+dy,0,sh-1),np.clip(ix+dx,0,sw-1)]>0
                yield x,y,values.astype(np.float32)
        return
    if operation == 'geometry.crop' and isinstance(step,CropTransform): pass
    elif operation == 'geometry.body' and isinstance(step,DenseWarpTransform):
        if (inverse_field is None or inverse_field.dtype != np.float32 or inverse_field.shape != (h,w,2)
                or step.pixel_centers is not None): raise ValueError('GENERATED_SUPPORT_BODY_FIELD')
    elif operation == 'image.outpaint' and isinstance(step,MatrixTransform):
        matrix = step.forward_matrix
        if matrix[0]!=1 or matrix[1]!=0 or matrix[3]!=0 or matrix[4]!=1 or matrix[6:]!=[0.,0.,1.] or matrix[2]!=int(matrix[2]) or matrix[5]!=int(matrix[5]):
            raise ValueError('GENERATED_SUPPORT_OUTPAINT_TRANSLATION')
    else: raise ValueError('GENERATED_SUPPORT_OPERATION_UNSUPPORTED')
    for y in range(0,h,tile_size):
        for x in range(0,w,tile_size):
            check_active(); th,tw=min(tile_size,h-y),min(tile_size,w-x)
            xx=np.arange(x,x+tw,dtype=np.float64)[None,:]; yy=np.arange(y,y+th,dtype=np.float64)[:,None]
            valid=np.ones((th,tw),bool)
            if isinstance(step,CropTransform):
                sx=xx+step.crop_rect.x; sy=yy+step.crop_rect.y
            elif isinstance(step,MatrixTransform):
                sx=xx-step.forward_matrix[2]; sy=yy-step.forward_matrix[5]
                valid &= (sx>=0)&(sx<sw)&(sy>=0)&(sy<sh)
            else:
                field=inverse_field[y:y+th,x:x+tw]
                if not np.isfinite(field).all(): raise ValueError('GENERATED_SUPPORT_NONFINITE_FIELD')
                # Exact body_warp.bilinear border semantics.
                sx=np.clip(field[...,0].astype(np.float64),0,sw-1)
                sy=np.clip(field[...,1].astype(np.float64),0,sh-1)
            ix=np.floor(sx).astype(np.int64); iy=np.floor(sy).astype(np.int64)
            fx=sx-ix; fy=sy-iy; output=np.zeros((th,tw),bool)
            for dy in (0,1):
                for dx in (0,1):
                    check_active(); cx=ix+dx;cy=iy+dy
                    weight=(fx if dx else 1-fx)*(fy if dy else 1-fy)
                    if isinstance(step,DenseWarpTransform):cx=np.clip(cx,0,sw-1);cy=np.clip(cy,0,sh-1)
                    support=(cx>=0)&(cx<sw)&(cy>=0)&(cy<sh)&(weight>0)
                    output |= support&(source[np.clip(cy,0,sh-1),np.clip(cx,0,sw-1)]>0)
            region=step.valid_region
            valid &= (xx>=region.x)&(xx<region.x+region.width)&(yy>=region.y)&(yy<region.y+region.height)
            yield x,y,(output&valid).astype(np.float32)

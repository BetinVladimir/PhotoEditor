"""Bounded mask patch selection without silently omitting effective edits."""
import numpy as np
import math


def plan_donor_mask_patch(target,source_face_diagonal,*,check_active):
    """Plan one bounded patch including all target support and Gaussian halo.

    Includes protected target support too, since it can affect color-ring blur.
    Halo conservatively covers float32 OpenCV auto kernels (4sigma plus2).
    Large/disjoint regions beyond the current patch budget reject explicitly.
    """
    if (not callable(check_active) or not isinstance(target,np.ndarray) or
            target.dtype!=np.float32 or target.ndim!=2 or min(target.shape)<1 or
            target.size>200_000_000 or not math.isfinite(source_face_diagonal) or
            source_face_diagonal<=0):
        raise ValueError('DONOR_MASK_PATCH')
    height,width=target.shape;x0,y0=width,height;x1=y1=0
    for y in range(0,height,512):
        for x in range(0,width,512):
            check_active();block=target[y:y+512,x:x+512]
            if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):
                raise ValueError('DONOR_MASK_PATCH')
            by,bx=np.nonzero(block>0)
            if len(bx):
                x0=min(x0,x+int(bx.min()));x1=max(x1,x+int(bx.max())+1)
                y0=min(y0,y+int(by.min()));y1=max(y1,y+int(by.max())+1)
    if x1==0:return (0,0),(1,1)
    halo=math.ceil(4*max(.01*source_face_diagonal,1.,.003*source_face_diagonal)+2)
    x0,y0=max(0,x0-halo),max(0,y0-halo)
    x1,y1=min(width,x1+halo),min(height,y1+halo)
    if (x1-x0)*(y1-y0)>1_048_576:raise ValueError('DONOR_MASK_PATCH_BUDGET')
    check_active()
    return (x0,y0),(x1-x0,y1-y0)


def select_donor_mask_patch(target,protected,origin,size,*,check_active):
    """Size is width,height; geometry/filter halo admission is a separate gate.

    Scan full masks in512x512 tiles before returning owned readonly patch masks.
    Every effective target pixel must be inside the selected patch. This does
    not establish eye anatomy, producer authority or sufficient Gaussian halo.
    """
    if (not callable(check_active) or not isinstance(target,np.ndarray) or
            target.ndim!=2 or target.dtype!=np.float32 or min(target.shape)<1 or
            target.size>200_000_000 or not isinstance(protected,np.ndarray) or
            protected.shape!=target.shape or protected.dtype!=np.float32 or
            not isinstance(origin,tuple) or not isinstance(size,tuple) or
            len(origin)!=2 or len(size)!=2 or
            any(type(v) is not int or v<0 for v in origin) or
            any(type(v) is not int or v<1 for v in size) or size[0]*size[1]>1_048_576):
        raise ValueError('DONOR_MASK_PATCH')
    x,y=origin;w,h=size;height,width=target.shape
    if x+w>width or y+h>height:raise ValueError('DONOR_MASK_PATCH')
    for ty in range(0,height,512):
        for tx in range(0,width,512):
            check_active()
            mask=target[ty:ty+512,tx:tx+512];guard=protected[ty:ty+512,tx:tx+512]
            for block in (mask,guard):
                if not np.isfinite(block).all() or np.any(block<0) or np.any(block>1):
                    raise ValueError('DONOR_MASK_PATCH')
            active=(mask>0)&(guard==0)
            iy0,iy1=max(y-ty,0),min(y+h-ty,mask.shape[0])
            ix0,ix1=max(x-tx,0),min(x+w-tx,mask.shape[1])
            if iy1>iy0 and ix1>ix0:active[iy0:iy1,ix0:ix1]=False
            if np.any(active):raise ValueError('DONOR_MASK_PATCH_INCOMPLETE')
    check_active()
    result=tuple(array[y:y+h,x:x+w].copy() for array in (target,protected))
    for array in result:array.setflags(write=False)
    return result

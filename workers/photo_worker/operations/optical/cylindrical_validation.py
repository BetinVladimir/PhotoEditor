"""Independent tiled geometry replay for retained cylindrical source fields."""
import numpy as np


def verify_cylindrical_files(files,source_shape,*,tile=256,check_active=lambda:None):
    if (not isinstance(source_shape,tuple) or len(source_shape)!=2
            or any(type(v) is not int or v<1 for v in source_shape) or source_shape[0]*source_shape[1]>200000000
            or type(tile) is not int or not 1<=tile<=512):raise ValueError('CYLINDRICAL_FIELD_INVALID')
    files.reservation.validate_file(files.inverse_field);files.reservation.validate_file(files.coverage)
    field=np.load(files.inverse_field,mmap_mode='r',allow_pickle=False)
    try:
        coverage=np.load(files.coverage,mmap_mode='r',allow_pickle=False)
        try:
            if field.dtype!=np.float32 or field.shape!=(*source_shape,2) or coverage.dtype!=np.bool_ or coverage.shape!=source_shape:
                raise ValueError('CYLINDRICAL_FIELD_INVALID')
            height,width=source_shape;focal=max(source_shape);cx,cy=width/2,height/2
            for y in range(0,height,tile):
                for x in range(0,width,tile):
                    check_active();h,w=min(tile,height-y),min(tile,width-x)
                    theta=(np.arange(x,x+w,dtype=np.float64)+.5-cx)/focal
                    sx=focal*np.tan(theta)+cx
                    sy=(np.arange(y,y+h,dtype=np.float64)[:,None]+.5-cy)/np.cos(theta)[None,:]+cy
                    valid=(np.abs(theta)[None,:]<np.pi/2)&(sx[None,:]>=.5)&(sx[None,:]<=width-.5)&(sy>=.5)&(sy<=height-.5)
                    expected=np.empty((h,w,2),np.float32)
                    expected[...,0]=np.where(valid,sx[None,:],0);expected[...,1]=np.where(valid,sy,0)
                    # Sampling coverage includes float32 field coordinate bounds.
                    visible=valid&(expected[...,0]>=.5)&(expected[...,0]<=width-.5)&(expected[...,1]>=.5)&(expected[...,1]<=height-.5)
                    actual=field[y:y+h,x:x+w]
                    if not np.array_equal(actual.view(np.uint32),expected.view(np.uint32)) or not np.array_equal(coverage[y:y+h,x:x+w],visible):
                        raise ValueError('CYLINDRICAL_FIELD_INVALID')
        finally:coverage._mmap.close()
    finally:field._mmap.close()
    check_active()

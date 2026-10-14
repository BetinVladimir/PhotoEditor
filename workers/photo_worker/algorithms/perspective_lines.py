"""Bounded deterministic line candidates from a float32 RGB working image.

This is evidence extraction, not homography acceptance or qualification. Weak,
transparent, texture-only and single-family scenes return no usable proposal.
Original HDR pixels are read only; clipping affects the detection proxy only.
"""
import math
import cv2
import numpy as np
from photo_worker.contracts.optical_perspective import LineSegments
from photo_worker.runtime.executor import TaskCancelled


def detect_perspective_lines(rgb, alpha=None, *, max_edge=1600, max_lines=128,
                             check_active=lambda: None):
    if not callable(check_active):
        raise ValueError('PERSPECTIVE_LINE_ACTIVITY_INVALID')
    supplied_check_active=check_active
    def check_active():
        if supplied_check_active() is False:
            raise TaskCancelled('Perspective line detection inactive')
    check_active()
    if (not isinstance(rgb, np.ndarray) or rgb.dtype != np.float32 or
            rgb.ndim != 3 or rgb.shape[2] != 3 or
            min(rgb.shape[:2]) < 2 or rgb.shape[0]*rgb.shape[1] > 200000000 or
            type(max_edge) is not int or not 64 <= max_edge <= 2048 or
            type(max_lines) is not int or not 4 <= max_lines <= 1000):
        raise ValueError('PERSPECTIVE_LINE_INPUT_INVALID')
    h, w = rgb.shape[:2]
    if alpha is not None and (not isinstance(alpha, np.ndarray) or alpha.shape != (h,w)
                              or alpha.dtype not in (np.dtype('float32'), np.dtype('uint16'))):
        raise ValueError('PERSPECTIVE_LINE_ALPHA_INVALID')
    scale=min(1., max_edge/max(h,w)); ph=max(2,round(h*scale));pw=max(2,round(w*scale))
    # Only proxy-size arrays allocated; source may be a read-only NPY mmap.
    xs=np.minimum(w-1,((np.arange(pw)+.5)*w/pw).astype(np.int64))
    ys=np.minimum(h-1,((np.arange(ph)+.5)*h/ph).astype(np.int64))
    proxy=rgb[ys[:,None],xs[None,:]]
    if not np.isfinite(proxy).all():raise ValueError('PERSPECTIVE_LINE_INPUT_INVALID')
    visible=None
    if alpha is not None:
        opacity=alpha[ys[:,None],xs[None,:]]
        if not np.isfinite(opacity).all() or alpha.dtype==np.float32 and (np.any(opacity<0) or np.any(opacity>1)):
            raise ValueError('PERSPECTIVE_LINE_ALPHA_INVALID')
        visible=opacity >= (.99 if alpha.dtype==np.float32 else 64879)
    gray=np.round(np.mean(np.clip(proxy,0,1),axis=2)*255).astype(np.uint8)
    if visible is not None:gray[~visible]=0
    check_active()
    detected=cv2.createLineSegmentDetector(cv2.LSD_REFINE_STD).detect(gray)[0]
    check_active()
    if detected is None:return None
    # Pathological texture is unsupported evidence. Bound Python candidate
    # storage/work independently of the source's pixel count.
    if detected.shape[0]>50000:return None
    candidates=[]
    for segment in detected[:,0,:]:
        check_active()
        x1,y1,x2,y2=map(float,segment);dx=x2-x1;dy=y2-y1;length=math.hypot(dx,dy)
        if (not all(math.isfinite(v) for v in segment) or length<.12*min(ph,pw) or
                not (0<=x1<pw and 0<=x2<pw and 0<=y1<ph and 0<=y2<ph)):continue
        angle=abs(math.degrees(math.atan2(dy,dx)))%180
        if min(angle,180-angle)<=35:family='horizontal'
        elif abs(angle-90)<=35:family='vertical'
        else:continue
        if visible is not None:
            # Sample at half-pixel intervals, bounded by the detection proxy.
            # Sparse samples can skip narrow transparent gaps in long edges.
            samples=2*math.ceil(max(abs(dx),abs(dy)))+1
            xx=np.clip(np.round(np.linspace(x1,x2,samples)).astype(int),0,pw-1)
            yy=np.clip(np.round(np.linspace(y1,y2,samples)).astype(int),0,ph-1)
            if any(not visible[np.clip(yy+oy,0,ph-1),np.clip(xx+ox,0,pw-1)].all()
                   for ox in range(-2,3) for oy in range(-2,3)):continue
        a=((x1+.5)/pw,(y1+.5)/ph);b=((x2+.5)/pw,(y2+.5)/ph)
        a=tuple(min(1.,max(0.,v)) for v in a);b=tuple(min(1.,max(0.,v)) for v in b)
        if b<a:a,b=b,a
        candidates.append((-length,family,a,b))
    candidates.sort()
    unique=[];buckets={}
    # Midpoint proximity alone cannot distinguish crossing lines. Compare
    # direction in proxy pixels, where the detector's angle is defined.
    def direction(item):
        _,_,a,b=item
        return math.atan2((b[1]-a[1])*ph,(b[0]-a[0])*pw)
    for item in candidates:
        check_active()
        _,family,a,b=item;mid=((a[0]+b[0])/2,(a[1]+b[1])/2)
        angle=direction(item)
        duplicate=False
        bx,by=math.floor(mid[0]/.03),math.floor(mid[1]/.03)
        neighbors=(other for ox in (-1,0,1) for oy in (-1,0,1)
                   for other in buckets.get((family,bx+ox,by+oy),()))
        for other in neighbors:
            _,f,c,d=other
            delta=abs(angle-direction(other))%math.pi
            if (min(delta,math.pi-delta)<=math.radians(5) and
                    math.hypot(mid[0]-(c[0]+d[0])/2,mid[1]-(c[1]+d[1])/2)<.03):
                duplicate=True;break
        if not duplicate:
            unique.append(item)
            buckets.setdefault((family,bx,by),[]).append(item)
    # Reserve two separated candidates per family before applying the global
    # cap. A facade with many long horizontals must retain vertical evidence.
    chosen=[]
    for family in ('horizontal','vertical'):
        axis=1 if family=='horizontal' else 0
        group=[item for item in unique if item[1]==family]
        if len(group)<2:return None
        def center(item):return (item[2][axis]+item[3][axis])/2
        low=min(group,key=lambda item:(center(item),item))
        high=min(group,key=lambda item:(-center(item),item))
        if center(high)-center(low)<.1:return None
        chosen.extend((low,high))
    reserved=set(chosen)
    for item in unique:
        check_active()
        if len(chosen)>=max_lines:break
        if item not in reserved:chosen.append(item)
    chosen.sort()
    check_active()
    return LineSegments.model_validate({'schema_version':1,'lines':[
        {'a':{'x':a[0],'y':a[1]},'b':{'x':b[0],'y':b[1]},'family':family}
        for _,family,a,b in chosen]})

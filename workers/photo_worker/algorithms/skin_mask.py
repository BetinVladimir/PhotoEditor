"""Conservative Face68 skin support, independent of inference providers."""
from dataclasses import dataclass
from collections.abc import Callable
import cv2
import numpy as np
from photo_worker.contracts.images import Face68,Rect

@dataclass(frozen=True)
class SkinMaskResult:
    skin:np.ndarray
    protected:np.ndarray
    face_width:float
    confidence:float
    reason:str|None


def build_skin_mask(face:Face68,roi:Rect,skin_probability:np.ndarray|None,*,manual:np.ndarray|None=None,
                    permanent:np.ndarray|None=None,manual_mode:bool=False,max_work_pixels:int=4194304,
                    check_active:Callable[[],None]=lambda:None)->SkinMaskResult:
    """All supplied probability arrays already refer to this exact source ROI.

    Automatic mode requires semantic skin parsing. Manual mode requires an
    explicit manual mask; it never bypasses feature protection. Manual masks
    restrict support and cannot paint beyond the conservative facial hull.
    """
    check_active();roi.validate_canvas(face.canvas)
    if type(max_work_pixels) is not int or not 1<=max_work_pixels<=4194304 or roi.width*roi.height>max_work_pixels:
        raise ValueError('Skin ROI budget exceeded')
    shape=(roi.height,roi.width)
    if manual_mode and manual is None:raise ValueError('Manual mode requires explicit mask')
    if not manual_mode and skin_probability is None:raise ValueError('Automatic skin parsing required')
    for source in (skin_probability,manual,permanent):
        if source is not None and (source.dtype!=np.float32 or source.shape!=shape or not np.isfinite(source).all()
                                   or np.any(source<0) or np.any(source>1)):
            raise ValueError('Invalid ROI probability mask')
    points=np.array([(p.x*face.canvas.width-.5-roi.x,p.y*face.canvas.height-.5-roi.y) for p in face.points],np.float64)
    width=float(np.max(points[:17,0])-np.min(points[:17,0]))
    confidence=min(p.confidence for p in face.points)
    skin=np.zeros(shape,np.float32);protected=np.zeros(shape,np.float32)
    def result(reason):
        skin.flags.writeable=False;protected.flags.writeable=False
        return SkinMaskResult(skin,protected,width,confidence,reason)
    height=float(np.ptp(points[:,1]))
    if width<=0 or height<=0 or max(width,height)>4*min(width,height):return result('degenerate_landmarks')
    if not manual_mode and confidence<.75:return result('low_confidence')
    if not manual_mode and width<64:return result('face_too_small')
    if np.any(points<0) or np.any(points[:,0]>=roi.width) or np.any(points[:,1]>=roi.height):
        return result('incomplete_face_roi')
    def hull(indices):
        mask=np.zeros(shape,np.uint8)
        polygon=cv2.convexHull(np.round(points[indices]).astype(np.int32))
        cv2.fillConvexPoly(mask,polygon,1)
        return mask
    support=hull(list(range(27))) # jaw and eyebrows, no invented forehead
    features=np.zeros(shape,np.uint8)
    for indices in (range(17,22),range(22,27),range(27,36),range(36,42),range(42,48),range(48,68)):
        check_active();features=np.maximum(features,hull(list(indices)))
    radius=max(1,round(.01*width))
    # Crop may be a very large canvas; radius remains bounded by ROI geometry.
    kernel=cv2.getStructuringElement(cv2.MORPH_ELLIPSE,(2*radius+1,2*radius+1))
    check_active();features=cv2.dilate(features,kernel)
    protected[:]=features
    if permanent is not None:np.maximum(protected,permanent,out=protected)
    allowed=support*(1-features)
    check_active();distance=cv2.distanceTransform(allowed,cv2.DIST_L2,cv2.DIST_MASK_PRECISE)
    skin[:]=np.minimum(distance/max(1.,.01*width),1.)
    if skin_probability is not None:skin*=skin_probability
    if manual is not None:skin*=manual
    # Partial protection is blended exactly once by Beauty/MaskComposer.
    skin[protected==1]=0
    check_active()
    return result(None if np.any(skin) else 'empty_skin')

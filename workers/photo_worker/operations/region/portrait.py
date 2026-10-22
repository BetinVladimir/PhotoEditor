"""Fresh pinned SFD/FAN/Face19 evidence for the immutable Region source."""
from contextlib import contextmanager
from types import SimpleNamespace
from uuid import NAMESPACE_URL, uuid5
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.images import Face68, NormalizedRect
from photo_worker.operations.face.detect import SFD_MEAN, _preview, _validated_candidates, _nms
from photo_worker.operations.face.landmarks import _crop, _points
from photo_worker.operations.learning.skin import open_holdout_skin
from photo_worker.operations.portrait.prepare import FaceInput


@contextmanager
def prepare_region_faces(rgb,alpha,image,pool,backend,model_lock_sha,scratch,*,
                         verify_runtime,check_active=lambda:None):
    """Use production detector/crop/parser math, without identity inference.

    Qualification covers all three models and their complete simultaneous peak.
    No submitted preview or old analysis becomes the evidence for these faces.
    """
    if pool.lock.lock_id!=model_lock_sha or backend not in ('cuda','mps'):
        raise ValueError('REGION_FACE_MODEL_LOCK_INVALID')
    if (not isinstance(rgb,np.ndarray) or rgb.dtype!=np.float32
            or rgb.shape!=(image.canvas.height,image.canvas.width,3)
            or rgb.shape[0]*rgb.shape[1]>200_000_000
            or alpha is not None and (not isinstance(alpha,np.ndarray)
                or alpha.dtype!=np.float32 or alpha.shape!=rgb.shape[:2])):
        raise ValueError('REGION_FACE_SOURCE_INVALID')
    verify_runtime();check_active()
    h,w=rgb.shape[:2]
    srgb,_=work_to_model(rgb)
    if alpha is not None:srgb=np.where((alpha>0)[...,None],srgb,0).astype(np.float32)
    preview=_preview(srgb,1024);ph,pw=preview.shape[:2]
    with pool.acquire('sfd',backend) as engine:
        raw=engine.infer(preview[...,::-1]*np.float32(255)-SFD_MEAN)
    verify_runtime();check_active()
    candidates=_validated_candidates(raw,pw,ph)
    selected,truncated=_nms(candidates[candidates[:,4]>=.8],.3,64)
    if truncated:raise ValueError('REGION_FACE_ANALYSIS_UNAVAILABLE')
    ordered=sorted(selected,key=lambda b:(float(b[1]),float(b[0]),float(b[3]),float(b[2]),-float(b[4])))
    observations=[];landmarks=[]
    with pool.acquire('fan2d',backend) as engine:
        for ordinal,box in enumerate(ordered):
            check_active()
            x,y,x2,y2,score=(float(value) for value in box)
            rect=NormalizedRect(x0=x/pw,y0=y/ph,x1=x2/pw,y1=y2/ph)
            signature=','.join(f'{value:.6f}' for value in (rect.x0,rect.y0,rect.x1,rect.y1))
            object_id=uuid5(NAMESPACE_URL,f'face:{image.canvas.source_sha256}:{model_lock_sha}:{ordinal}:{signature}')
            crop,geometry=_crop(srgb,rect,256,1.25)
            points=_points(engine.infer(crop),w,h,geometry)
            face=Face68(object_id=object_id,points=points,canvas=image.canvas,transform_chain=image.transform_chain)
            if float(np.mean([point.confidence for point in points],dtype=np.float64))<.75:
                raise ValueError('REGION_FACE_ANALYSIS_UNAVAILABLE')
            landmarks.append(face)
            # Private parser input, not a persisted/claimed identity observation.
            observations.append(SimpleNamespace(face_id=object_id,box=rect))
    del srgb,preview
    verify_runtime();check_active()
    with open_holdout_skin(rgb,SimpleNamespace(faces=observations),pool,backend,
            model_lock_sha,scratch,check_active) as skin:
        verify_runtime();check_active()
        yield tuple(FaceInput(face,skin) for face in landmarks)
        check_active();verify_runtime()

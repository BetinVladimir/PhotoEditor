"""Pinned SFD face detection adapter with deterministic NMS and object identity."""
from __future__ import annotations
from contextlib import ExitStack
from typing import Protocol
from uuid import NAMESPACE_URL,uuid5
import cv2
import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.face import FaceBoxes,FaceDetectParams
from photo_worker.contracts.images import FaceBox,NormalizedRect,WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from .resources import required_face_analysis_ram


class SFDEngine(Protocol):
    def infer(self,bgr_mean_subtracted:np.ndarray)->np.ndarray:...


SFD_MEAN=np.array([104.,117.,123.],np.float32)


def _preview(rgb:np.ndarray,long_side:int)->np.ndarray:
    h,w=rgb.shape[:2];scale=min(1.,long_side/max(h,w));size=(max(1,round(w*scale)),max(1,round(h*scale)))
    return cv2.resize(rgb,size,interpolation=cv2.INTER_AREA if scale<1 else cv2.INTER_LINEAR).astype(np.float32)


def _nms(candidates:np.ndarray,threshold:float,maximum:int):
    value=np.asarray(candidates,np.float32)
    if value.ndim!=2 or value.shape[1]!=5 or len(value)>100000 or not np.isfinite(value).all():raise ValueError('MODEL_OUTPUT_INVALID')
    if np.any(value[:,4]<0) or np.any(value[:,4]>1):raise ValueError('MODEL_OUTPUT_INVALID')
    order=np.lexsort((np.arange(len(value)),value[:,2],value[:,3],value[:,0],value[:,1],-value[:,4]))
    keep=[]
    while len(order):
        i=int(order[0]);keep.append(i)
        if len(keep)>=maximum or len(order)==1:break
        rest=order[1:];xx1=np.maximum(value[i,0],value[rest,0]);yy1=np.maximum(value[i,1],value[rest,1])
        xx2=np.minimum(value[i,2],value[rest,2]);yy2=np.minimum(value[i,3],value[rest,3])
        inter=np.maximum(0,xx2-xx1)*np.maximum(0,yy2-yy1)
        area_i=(value[i,2]-value[i,0])*(value[i,3]-value[i,1]);area_r=(value[rest,2]-value[rest,0])*(value[rest,3]-value[rest,1])
        union=area_i+area_r-inter;iou=np.divide(inter,union,out=np.ones_like(inter),where=union>0)
        order=rest[iou<=threshold]
    return value[keep],len(keep)>=maximum and len(order)>1


def _validated_candidates(raw:object,width:int,height:int)->np.ndarray:
    """Reject the entire native SFD response before score filtering."""
    if (not isinstance(raw,np.ndarray) or raw.ndim!=2 or
            raw.shape[1]!=5 or len(raw)>100000):
        raise ValueError('MODEL_OUTPUT_INVALID')
    try:
        value=np.asarray(raw,dtype=np.float32)
    except (TypeError,ValueError,OverflowError):
        raise ValueError('MODEL_OUTPUT_INVALID') from None
    if (value.ndim!=2 or value.shape[1]!=5 or len(value)>100000 or
            not np.isfinite(value).all() or np.any(value[:,4]<0) or
            np.any(value[:,4]>1) or np.any(value[:,:2]<0) or
            np.any(value[:,2]>width) or np.any(value[:,3]>height) or
            np.any(value[:,2]<=value[:,0]) or
            np.any(value[:,3]<=value[:,1])):
        raise ValueError('MODEL_OUTPUT_INVALID')
    return value


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(FaceDetectParams)
        if params.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        image=artifacts.contract(params.image,WorkingImage)
        if (image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or image.color.encoding!='gamma24' or image.canvas.width*image.canvas.height>200000000):raise ValueError('FACE_IMAGE_INVALID')
        artifacts.require_declared_input(image.pixels)
        if image.alpha is not None:artifacts.require_declared_input(image.alpha)
        h,w=image.canvas.height,image.canvas.width
        if ram<required_face_analysis_ram(h,w,image.alpha is not None):raise OperationFailure('RESOURCE_OOM')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,3)
    try:
        with ExitStack() as stack:
            local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'))
            rgb=stack.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
            alpha=None
            if image.alpha is not None:
                local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'))
                alpha=stack.enter_context(read_alpha_array(local.path,(h,w),ctx.check_active))
            srgb,clipped=work_to_model(rgb)
            if alpha is not None:srgb=np.where((alpha>0)[...,None],srgb,0).astype(np.float32)
            alpha_excluded=float(np.count_nonzero(alpha==0)/(h*w)) if alpha is not None else 0.
            preview=_preview(srgb,params.long_side);ph,pw=preview.shape[:2]
            model_input=preview[...,::-1]*np.float32(255)-SFD_MEAN;ctx.progress(1,3)
            with pool.acquire(params.model_id,backend) as engine:raw_candidates=engine.infer(model_input)
            ctx.check_active()
            candidates=_validated_candidates(raw_candidates,pw,ph)
            candidates=candidates[candidates[:,4]>=params.score_threshold]
            kept,truncated=_nms(candidates,params.nms_iou,params.max_faces)
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    ordered=sorted(kept,key=lambda b:(float(b[1]),float(b[0]),float(b[3]),float(b[2]),-float(b[4])))
    faces=[]
    for ordinal,b in enumerate(ordered):
        x,y,x2,y2,score=(float(v) for v in b);rect=NormalizedRect(x0=x/pw,y0=y/ph,x1=x2/pw,y1=y2/ph)
        signature=','.join(f'{v:.6f}' for v in (rect.x0,rect.y0,rect.x1,rect.y1))
        oid=uuid5(NAMESPACE_URL,f'face:{image.canvas.source_sha256}:{params.model_lock_id}:{ordinal}:{signature}')
        faces.append(FaceBox(object_id=oid,bbox_normalized=rect,confidence=score))
    warnings=[]
    if not faces:warnings.append('no_faces')
    if truncated:warnings.append('faces_truncated')
    if np.count_nonzero(clipped):warnings.append('source_srgb_clipped')
    result=FaceBoxes(image=params.image,faces=faces,model_id=params.model_id,model_lock_id=params.model_lock_id,backend=backend,
        preprocessing_id=params.preprocessing_id,preview_width=pw,preview_height=ph,candidate_count=len(candidates),accepted_count=len(faces),alpha_excluded_fraction=alpha_excluded,warnings=warnings)
    ctx.check_active();ref=artifacts.stage_contract(result,'face_boxes');ctx.progress(3,3)
    return OperationResult(outputs=[ref],review_required=bool(warnings))


def face_detect_operations(config:ArtifactConfig,pool:ModelPool,backend:str)->list[Operation]:
    def detect(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'face_boxes'})) as artifacts:return execute(ctx,artifacts,pool,backend)
    return [Operation('face.detect','gpu',detect)]

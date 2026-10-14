"""Pinned FAN 2D face-landmark adapter with explicit crop-to-source mapping."""
from __future__ import annotations
from contextlib import ExitStack
from typing import Protocol
import cv2
import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.face import FaceBoxes,FaceLandmarks,LandmarkParams
from photo_worker.contracts.images import Face68,Landmark,WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array,read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from .resources import required_face_analysis_ram


class FANEngine(Protocol):
    def infer(self,crop_rgb:np.ndarray)->np.ndarray:...


def _crop(rgb:np.ndarray,box,size:int,expansion:float):
    h,w=rgb.shape[:2];x0,x1=box.x0*w,box.x1*w;y0,y1=box.y0*h,box.y1*h
    side=max(x1-x0,y1-y0)*expansion;cx=(x0+x1)/2;cy=(y0+y1)/2
    left,top=cx-side/2,cy-side/2
    matrix=np.array([[side/size,0,left],[0,side/size,top]],np.float32)
    inverse=cv2.invertAffineTransform(matrix)
    crop=cv2.warpAffine(rgb,inverse,(size,size),flags=cv2.INTER_LINEAR,borderMode=cv2.BORDER_REFLECT_101)
    return crop.astype(np.float32),(left,top,side)


def _points(raw,width:int,height:int,geometry):
    if not isinstance(raw,np.ndarray) or raw.shape!=(68,3):
        raise ValueError('MODEL_OUTPUT_INVALID')
    value=np.asarray(raw,np.float32)
    if value.shape!=(68,3) or not np.isfinite(value).all() or np.any(value[:,2]<0) or np.any(value[:,2]>1) or np.any(value[:,:2]<0) or np.any(value[:,:2]>255):raise ValueError('MODEL_OUTPUT_INVALID')
    left,top,side=geometry
    result=[]
    for x,y,confidence in value:
        sx=float(np.clip((left+(float(x)+.5)*side/256)/width,0,1));sy=float(np.clip((top+(float(y)+.5)*side/256)/height,0,1))
        result.append(Landmark(x=sx,y=sy,confidence=float(confidence)))
    return result


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(LandmarkParams)
        if params.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        image=artifacts.contract(params.image,WorkingImage);faces=artifacts.contract(params.faces,FaceBoxes)
        if (faces.image!=params.image or image.dtype!='float32'
                or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24'
                or image.canvas.width*image.canvas.height>200000000):raise ValueError('LANDMARK_BINDING_INVALID')
        required=(params.image,image.pixels,*(([image.alpha] if image.alpha else [])),params.faces)
        for ref in required:artifacts.require_declared_input(ref)
        h,w=image.canvas.height,image.canvas.width
        if ram<required_face_analysis_ram(h,w,image.alpha is not None):raise OperationFailure('RESOURCE_OOM')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,max(1,len(faces.faces)+1))
    try:
        with ExitStack() as stack:
            local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'))
            rgb=stack.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
            srgb,clipped=work_to_model(rgb)
            if image.alpha is not None:
                local=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'))
                alpha=stack.enter_context(read_alpha_array(local.path,(h,w),ctx.check_active));srgb=np.where((alpha>0)[...,None],srgb,0).astype(np.float32)
            landmarks=[]
            with pool.acquire(params.model_id,backend) as engine:
                for index,face in enumerate(faces.faces):
                    ctx.check_active();crop,geometry=_crop(srgb,face.bbox_normalized,params.crop_size,params.expansion)
                    raw=engine.infer(crop);ctx.check_active()
                    points=_points(raw,w,h,geometry)
                    landmarks.append(Face68(object_id=face.object_id,points=points,canvas=image.canvas,transform_chain=image.transform_chain));ctx.progress(index+1,len(faces.faces)+1)
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    mean=float(np.mean([p.confidence for face in landmarks for p in face.points],dtype=np.float64)) if landmarks else 0.
    warnings=[]
    if not landmarks:warnings.append('no_faces')
    if landmarks and mean<.75:warnings.append('low_confidence')
    if np.count_nonzero(clipped):warnings.append('source_srgb_clipped')
    result=FaceLandmarks(image=params.image,faces=params.faces,landmarks=landmarks,model_id=params.model_id,model_lock_id=params.model_lock_id,backend=backend,preprocessing_id=params.preprocessing_id,mean_confidence=mean,warnings=warnings)
    ctx.check_active();ref=artifacts.stage_contract(result,'face_landmarks');ctx.progress(len(faces.faces)+1,len(faces.faces)+1)
    return OperationResult(outputs=[ref],review_required=bool(warnings))


def landmark_operations(config:ArtifactConfig,pool:ModelPool,backend:str)->list[Operation]:
    def landmarks(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'face_landmarks'})) as artifacts:return execute(ctx,artifacts,pool,backend)
    return [Operation('face.landmarks','gpu',landmarks)]

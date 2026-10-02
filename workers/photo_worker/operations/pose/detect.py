"""Version-locked torchvision Keypoint R-CNN adapter producing source-bound Pose17 contracts."""
from __future__ import annotations
from contextlib import ExitStack
from typing import Protocol
from uuid import NAMESPACE_URL,uuid5
import numpy as np

from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.images import Landmark,Pose17,WorkingImage
from photo_worker.contracts.pose import PoseParams,PoseSet
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


class PoseEngine(Protocol):
    def infer(self,rgb:np.ndarray)->dict[str,np.ndarray]:...


def _detections(raw:dict[str,np.ndarray],width:int,height:int,threshold:float,maximum:int,source_sha:str,lock_id:str):
    if not isinstance(raw,dict) or not {'boxes','scores','keypoints','keypoint_scores'}<=set(raw):raise ValueError('MODEL_OUTPUT_INVALID')
    boxes=np.asarray(raw['boxes'],np.float32);scores=np.asarray(raw['scores'],np.float32)
    points=np.asarray(raw['keypoints'],np.float32);point_scores=np.asarray(raw['keypoint_scores'],np.float32)
    n=len(scores)
    if n>10000 or boxes.shape!=(n,4) or points.shape!=(n,17,3) or point_scores.shape!=(n,17) or not all(np.isfinite(x).all() for x in (boxes,scores,points,point_scores)):
        raise ValueError('MODEL_OUTPUT_INVALID')
    if np.any(scores<0) or np.any(scores>1) or np.any(point_scores<0) or np.any(point_scores>1):raise ValueError('MODEL_OUTPUT_INVALID')
    order=sorted((i for i in range(n) if scores[i]>=threshold),key=lambda i:(float(boxes[i,1]),float(boxes[i,0]),float(boxes[i,3]),float(boxes[i,2]),i))
    truncated=len(order)>maximum;order=order[:maximum];out=[]
    for ordinal,i in enumerate(order):
        x1,y1,x2,y2=(float(v) for v in boxes[i])
        if not (0<=x1<x2<=width and 0<=y1<y2<=height):raise ValueError('MODEL_OUTPUT_INVALID')
        signature=','.join(f'{v:.6f}' for v in (x1/width,y1/height,x2/width,y2/height))
        object_id=uuid5(NAMESPACE_URL,f'pose:{source_sha}:{lock_id}:{ordinal}:{signature}')
        landmarks=[]
        for j in range(17):
            x=float(np.clip(points[i,j,0]/width,0,1));y=float(np.clip(points[i,j,1]/height,0,1))
            confidence=float(point_scores[i,j]);landmarks.append(Landmark(x=x,y=y,confidence=confidence))
        out.append((object_id,landmarks,float(scores[i])))
    return out,n,truncated


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<2147483648:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(PoseParams)
        if params.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        image=artifacts.contract(params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('POSE_IMAGE_INVALID')
        if image.pixels not in ctx.lease.command.inputs or image.alpha is not None and image.alpha not in ctx.lease.command.inputs:raise ValueError('POSE_INPUT_UNDECLARED')
        h,w=image.canvas.height,image.canvas.width
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
                alpha=stack.enter_context(read_array(local.path,(h,w),'uint16',ctx.check_active))
            srgb,clipped=work_to_model(rgb)
            if alpha is not None:srgb=np.where((alpha>0)[...,None],srgb,0).astype(np.float32)
            alpha_excluded=float(np.count_nonzero(alpha==0)/(h*w)) if alpha is not None else 0.
            ctx.progress(1,3)
            with pool.acquire(params.model_id,backend) as engine:raw=engine.infer(srgb)
            accepted,detected,truncated=_detections(raw,w,h,params.score_threshold,params.max_people,image.canvas.source_sha256,params.model_lock_id)
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    poses=[Pose17(object_id=oid,points=points,canvas=image.canvas,transform_chain=image.transform_chain) for oid,points,_ in accepted]
    warnings=[]
    if not poses:warnings.append('no_people')
    if truncated:warnings.append('people_truncated')
    if poses and min((sum(p.confidence for p in pose.points)/17 for pose in poses),default=1)<.75:warnings.append('low_keypoint_confidence')
    if np.count_nonzero(clipped):warnings.append('source_srgb_clipped')
    result=PoseSet(image=params.image,poses=poses,model_id=params.model_id,model_lock_id=params.model_lock_id,backend=backend,
        preprocessing_id=params.preprocessing_id,detected_count=detected,accepted_count=len(poses),alpha_excluded_fraction=alpha_excluded,warnings=warnings)
    ref=artifacts.stage_contract(result,'pose_set');ctx.progress(3,3)
    return OperationResult(outputs=[ref],review_required=bool(warnings))


def pose_operations(config:ArtifactConfig,pool:ModelPool,backend:str)->list[Operation]:
    def detect(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'pose_set'})) as artifacts:return execute(ctx,artifacts,pool,backend)
    return [Operation('pose.detect','gpu',detect)]

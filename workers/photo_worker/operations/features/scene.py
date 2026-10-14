from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.scene import Embedding384,SceneParams
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _required_embedding_ram(height:int,width:int)->int:
    if type(height) is not int or type(width) is not int or height<1 or width<1:raise ValueError('SCENE_SIZE_INVALID')
    return 512*1024**2+height*width*96


def pad_preview(rgb,size=518):
    h,w=rgb.shape[:2];scale=size/max(h,w);nw,nh=max(1,round(w*scale)),max(1,round(h*scale));small=cv2.resize(rgb,(nw,nh),interpolation=cv2.INTER_AREA if scale<1 else cv2.INTER_CUBIC);out=np.zeros((size,size,3),np.float32);x=(size-nw)//2;y=(size-nh)//2;out[y:y+nh,x:x+nw]=small;return out


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(SceneParams);image=artifacts.contract(p.image,WorkingImage)
        if p.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        if (image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or image.color.encoding!='gamma24' or image.canvas.width*image.canvas.height>200000000
                or image.pixels not in ctx.lease.command.inputs):raise ValueError('SCENE_INPUT_INVALID')
        if ram<_required_embedding_ram(image.canvas.height,image.canvas.width):raise OperationFailure('RESOURCE_OOM')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    try:
        with ExitStack() as stack:
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(image.canvas.height,image.canvas.width,3),'float32',ctx.check_active,unit_interval=True));srgb,clipped=work_to_model(rgb);preview=pad_preview(srgb,p.input_size)
            with pool.acquire(p.model_id,backend) as engine:value=np.asarray(engine.infer(preview),np.float32).reshape(-1)
        if value.shape!=(384,) or not np.isfinite(value).all():raise ValueError('MODEL_OUTPUT_INVALID')
        norm=float(np.linalg.norm(value))
        if norm<=1e-12:raise ValueError('MODEL_OUTPUT_INVALID')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    result=Embedding384(image=p.image,values=(value/norm).tolist(),model_id=p.model_id,model_lock_id=p.model_lock_id,backend=backend,preprocessing_id=p.preprocessing_id,source_clip_fraction=float(np.mean(clipped)));ctx.check_active();ref=artifacts.stage_contract(result,'scene_embedding');return OperationResult(outputs=[ref],review_required=False,bytes_written=len(canonical_bytes(result)))


def scene_operations(config:ArtifactConfig,pool:ModelPool,backend:str):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'scene_embedding'})) as a:return execute(ctx,a,pool,backend)
    return [Operation('features.scene','gpu',run)]

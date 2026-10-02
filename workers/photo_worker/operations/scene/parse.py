from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.semantic import SceneLabels,SceneParseParams
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def decode_logits(logits,w,h):
    value=np.asarray(logits,np.float32)
    if value.ndim!=3 or value.shape[0]!=150 or not np.isfinite(value).all():raise ValueError('MODEL_OUTPUT_INVALID')
    maximum=np.max(value,axis=0);exp=np.exp(value-maximum);prob=exp/np.sum(exp,axis=0);labels=np.argmax(prob,axis=0).astype(np.uint16);confidence=float(np.mean(np.max(prob,axis=0)));return cv2.resize(labels,(w,h),interpolation=cv2.INTER_NEAREST).astype(np.uint16),confidence


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(SceneParseParams);image=artifacts.contract(p.image,WorkingImage)
        if p.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        if image.dtype!='float32' or image.canvas.width*image.canvas.height>200000000:raise ValueError('Invalid scene image')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width
    try:
        with ExitStack() as stack:
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));srgb,clipped=work_to_model(rgb)
            with pool.acquire(p.model_id,backend) as engine:logits=engine.infer(srgb)
        labels,confidence=decode_logits(logits,w,h)
        with artifacts.scratch.reserve(h*w*2+8192) as reservation:
            path=reservation.path('scene-labels','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<u2',shape=(h,w));mm[:]=labels;mm.flush();mm._mmap.close();reservation.validate();lref=artifacts.stage(path,reservation,'scene_labels','application/octet-stream');written=path.stat().st_size
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    warnings=[]
    if confidence<.75:warnings.append('low_confidence')
    if np.any(clipped):warnings.append('source_srgb_clipped')
    result=SceneLabels(image=p.image,labels=lref,canvas=image.canvas,transform_chain=image.transform_chain,model_id=p.model_id,model_lock_id=p.model_lock_id,backend=backend,preprocessing_id=p.preprocessing_id,confidence=confidence,warnings=warnings);ref=artifacts.stage_contract(result,'scene_labels_ref');return OperationResult(outputs=[lref,ref],review_required=bool(warnings),bytes_written=written+len(canonical_bytes(result)))


def scene_parse_operations(config:ArtifactConfig,pool:ModelPool,backend:str):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'scene_labels','scene_labels_ref'})) as a:return execute(ctx,a,pool,backend)
    return [Operation('scene.parse','gpu',run)]

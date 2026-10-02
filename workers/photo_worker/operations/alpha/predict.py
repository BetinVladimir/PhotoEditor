from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.mask_png import png16_bound,write_png16
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.semantic import AlphaParams,AlphaPrediction
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult

MEAN=np.array([.485,.456,.406],np.float32);STD=np.array([.229,.224,.225],np.float32)


def normalize_output(raw):
    value=np.asarray(raw,np.float32)
    if value.shape!=(320,320) or not np.isfinite(value).all():raise ValueError('MODEL_OUTPUT_INVALID')
    lo,hi=float(value.min()),float(value.max())
    return np.zeros_like(value) if hi-lo<=1e-12 else ((value-lo)/(hi-lo)).astype(np.float32)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(AlphaParams);image=artifacts.contract(p.image,WorkingImage)
        if p.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        if image.dtype!='float32' or image.canvas.width*image.canvas.height>200000000:raise ValueError('Invalid alpha image')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width
    try:
        with ExitStack() as stack:
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));srgb,clipped=work_to_model(rgb);preview=cv2.resize(srgb,(320,320),interpolation=cv2.INTER_AREA if max(h,w)>320 else cv2.INTER_CUBIC);model_input=(preview-MEAN)/STD
            with pool.acquire(p.model_id,'cpu') as engine:small=normalize_output(engine.infer(model_input))
        alpha=cv2.resize(small,(w,h),interpolation=cv2.INTER_CUBIC).clip(0,1).astype(np.float32)
        if image.alpha is not None:
            with artifacts.input(image.alpha,suffix='.npy') as local,read_array(local.path,(h,w),'uint16',ctx.check_active) as source_alpha:alpha*=np.asarray(source_alpha,np.float32)/65535
        bound=png16_bound(w,h)
        with artifacts.scratch.reserve(h*w*4+8192+bound) as reservation:
            path=reservation.path('predicted-alpha','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w));mm[:]=alpha;mm.flush();mm._mmap.close();png=reservation.path('predicted-alpha','.png')
            with artifacts.scratch.open_writer(png,reservation) as stream:write_png16(alpha,stream,max_bytes=bound,check_active=ctx.check_active)
            reservation.validate();aref=artifacts.stage(path,reservation,'alpha_mask','application/octet-stream');pref=artifacts.stage(png,reservation,'alpha_mask_png','image/png');written=path.stat().st_size+png.stat().st_size
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    foreground=float(np.mean(alpha>=.5));confidence=float(np.mean(np.maximum(alpha,1-alpha)));warnings=[]
    if foreground<.001:warnings.append('empty_foreground')
    if confidence<.75:warnings.append('low_confidence')
    if np.any(clipped):warnings.append('source_srgb_clipped')
    mask=MaskRef(artifact=aref,canvas=image.canvas,transform_chain=image.transform_chain,selector='alpha',confidence=confidence,model_lock_id=p.model_lock_id,protected=False);mref=artifacts.stage_contract(mask,'alpha_mask_ref');report=AlphaPrediction(image=p.image,alpha=mask,model_id=p.model_id,model_lock_id=p.model_lock_id,backend='cpu',preprocessing_id=p.preprocessing_id,foreground_fraction=foreground,source_clip_fraction=float(np.mean(clipped)),warnings=warnings);rref=artifacts.stage_contract(report,'alpha_report')
    return OperationResult(outputs=[aref,pref,mref,rref],review_required=bool(warnings),bytes_written=written+len(canonical_bytes(mask))+len(canonical_bytes(report)))


def alpha_operations(config:ArtifactConfig,pool:ModelPool):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'alpha_mask','alpha_mask_png','alpha_mask_ref','alpha_report'})) as a:return execute(ctx,a,pool)
    return [Operation('alpha.predict','cpu',run)]

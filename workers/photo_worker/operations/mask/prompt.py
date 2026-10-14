from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.mask_png import png16_bound,write_png16
from photo_worker.algorithms.model_delta import work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import MaskRef,WorkingImage
from photo_worker.contracts.prompt_mask import PromptMaskParams,PromptMaskReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _required_prompt_ram(height:int,width:int)->int:
    """Reserve whole-frame color conversion, mask resize and PNG staging."""
    if type(height) is not int or type(width) is not int or height<1 or width<1:raise ValueError('PROMPT_SIZE_INVALID')
    return 512*1024**2+height*width*128


def preprocess(rgb,size=1024):
    h,w=rgb.shape[:2];scale=size/max(h,w);nw,nh=max(1,round(w*scale)),max(1,round(h*scale));out=np.zeros((size,size,3),np.float32);out[:nh,:nw]=cv2.resize(rgb,(nw,nh),interpolation=cv2.INTER_AREA if scale<1 else cv2.INTER_CUBIC);return out,scale,nw,nh


def select_candidate(raw_masks,raw_scores):
    if not isinstance(raw_masks,np.ndarray) or not isinstance(raw_scores,np.ndarray):
        raise ValueError('MODEL_OUTPUT_INVALID')
    shape=raw_masks.shape
    if len(shape)>5:
        raise ValueError('MODEL_OUTPUT_INVALID')
    while len(shape)>3 and shape[0]==1:shape=shape[1:]
    if len(shape)==2:shape=(1,*shape)
    if (len(shape)!=3 or not 1<=shape[0]<=16 or
            not 1<=shape[1]<=1024 or not 1<=shape[2]<=1024 or
            raw_scores.shape not in ((shape[0],),(1,shape[0]))):
        raise ValueError('MODEL_OUTPUT_INVALID')
    masks=np.asarray(raw_masks,np.float32);scores=np.asarray(raw_scores,np.float32).reshape(-1)
    while masks.ndim>3 and masks.shape[0]==1:masks=masks[0]
    if masks.ndim==2:masks=masks[None]
    if (masks.shape!=shape or scores.shape!=(shape[0],) or
            not np.isfinite(masks).all() or not np.isfinite(scores).all() or
            np.any(scores<0) or np.any(scores>1)):
        raise ValueError('MODEL_OUTPUT_INVALID')
    probabilities=1/(1+np.exp(-np.clip(masks,-30,30))) if np.any((masks<0)|(masks>1)) else masks
    index=min(range(len(scores)),key=lambda i:(-float(scores[i]),i));return probabilities[index].astype(np.float32),float(np.clip(scores[index],0,1)),index,len(scores)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<1073741824:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(PromptMaskParams);image=artifacts.contract(p.image,WorkingImage)
        if p.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        if (p.source_sha256!=image.canvas.source_sha256 or image.dtype!='float32'
                or image.color.profile_id!='work_rec2020_gamma24_v1' or image.color.encoding!='gamma24'
                or image.canvas.width*image.canvas.height>200000000
                or image.pixels not in ctx.lease.command.inputs):raise ValueError('PROMPT_SOURCE_INVALID')
        if ram<_required_prompt_ram(image.canvas.height,image.canvas.width):raise OperationFailure('RESOURCE_OOM')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width
    try:
        with ExitStack() as stack:
            rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));srgb,_=work_to_model(rgb);preview,scale,nw,nh=preprocess(srgb);points=np.asarray([[q.x*w*scale,q.y*h*scale] for q in p.points],np.float32);labels=np.asarray([1 if q.label=='positive' else 0 for q in p.points],np.float32);box=np.asarray([0,0,nw-1,nh-1] if p.box is None else [p.box.x0*w*scale,p.box.y0*h*scale,p.box.x1*w*scale,p.box.y1*h*scale],np.float32)
            with pool.acquire(p.model_id,backend) as engine:raw,scores=engine.infer(preview,points,labels,box)
            ctx.check_active()
        small,score,index,count=select_candidate(raw,scores);small=cv2.resize(small,(1024,1024),interpolation=cv2.INTER_LINEAR) if small.shape!=(1024,1024) else small;mask=cv2.resize(small[:nh,:nw],(w,h),interpolation=cv2.INTER_LINEAR).clip(0,1).astype(np.float32)
        bound=png16_bound(w,h)
        with artifacts.scratch.reserve(h*w*4+8192+bound) as reservation:
            path=reservation.path('prompt-mask','.npy');mm=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w))
            try:mm[:]=mask;mm.flush();ctx.check_active()
            finally:mm._mmap.close()
            png=reservation.path('prompt-mask','.png')
            with artifacts.scratch.open_writer(png,reservation) as stream:write_png16(mask,stream,max_bytes=bound,check_active=ctx.check_active)
            reservation.validate();ctx.check_active();aref=artifacts.stage(path,reservation,'prompt_mask','application/octet-stream');ctx.check_active();pref=artifacts.stage(png,reservation,'prompt_mask_png','image/png');written=path.stat().st_size+png.stat().st_size
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    warnings=[]
    if score<.75:warnings.append('low_confidence')
    if np.mean(mask>=.5)<.0001:warnings.append('empty_mask')
    target=MaskRef(artifact=aref,canvas=image.canvas,transform_chain=image.transform_chain,object_id=p.object_id,selector=p.selector,confidence=score,model_lock_id=p.model_lock_id,protected=False);ctx.check_active();mref=artifacts.stage_contract(target,'prompt_mask_ref');report=PromptMaskReport(image=p.image,mask=target,candidate_count=count,selected_index=index,score=score,model_id=p.model_id,model_lock_id=p.model_lock_id,backend=backend,preprocessing_id=p.preprocessing_id,warnings=warnings);ctx.check_active();rref=artifacts.stage_contract(report,'prompt_mask_report')
    return OperationResult(outputs=[aref,pref,mref,rref],review_required=bool(warnings),bytes_written=written+len(canonical_bytes(target))+len(canonical_bytes(report)))


def prompt_operations(config:ArtifactConfig,pool:ModelPool,backend:str):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'prompt_mask','prompt_mask_png','prompt_mask_ref','prompt_mask_report'})) as a:return execute(ctx,a,pool,backend)
    return [Operation('mask.prompt','gpu',run)]

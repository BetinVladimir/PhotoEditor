"""Deterministic local SD1.5 inpaint candidate with exact protected restoration."""
from __future__ import annotations
from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import transfer_model_delta,work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.restoration import GenerationParams,GenerationReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult

def _square_roi(mask,context=64):
    h,w=mask.shape;ys,xs=np.nonzero(mask)
    if not len(xs):raise ValueError('GENERATION_MASK_EMPTY')
    x0=max(0,int(xs.min())-context);x1=min(w,int(xs.max())+1+context);y0=max(0,int(ys.min())-context);y1=min(h,int(ys.max())+1+context)
    side=max(x1-x0,y1-y0);cx=(x0+x1)/2;cy=(y0+y1)/2
    x0=max(0,min(w-side,int(round(cx-side/2))));y0=max(0,min(h-side,int(round(cy-side/2))))
    return x0,y0,min(w,x0+side),min(h,y0+side)

def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<2147483648:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(GenerationParams)
        if params.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        image=artifacts.contract(params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('GENERATION_IMAGE_INVALID')
        masks=[params.mask,*params.protected]
        if any(m.canvas!=image.canvas or m.transform_chain!=image.transform_chain for m in masks):raise ValueError('GENERATION_MASK_GEOMETRY')
        required=[image.pixels,params.mask.artifact,*[m.artifact for m in params.protected],*([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('GENERATION_INPUT_UNDECLARED')
        h,w=image.canvas.height,image.canvas.width
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,5)
    try:
        with pool.acquire(params.model_id,backend) as engine,ExitStack() as stack:
            def mapped(ref,shape):
                local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
                return stack.enter_context(read_array(local.path,shape,'float32',ctx.check_active,unit_interval=True))
            source=mapped(image.pixels,(h,w,3));target=np.asarray(mapped(params.mask.artifact,(h,w)))>=.5;protected=np.zeros((h,w),bool)
            for item in params.protected:protected|=np.asarray(mapped(item.artifact,(h,w)))>=.5
            effective=target&~protected;x0,y0,x1,y1=_square_roi(effective);roi=np.asarray(source[y0:y1,x0:x1],np.float32);mask=effective[y0:y1,x0:x1]
            model_input,_=work_to_model(roi);input512=cv2.resize(model_input,(512,512),interpolation=cv2.INTER_AREA if max(roi.shape[:2])>512 else cv2.INTER_CUBIC)
            mask512=cv2.resize(mask.astype(np.uint8),(512,512),interpolation=cv2.INTER_NEAREST).astype(np.float32)
            generated=np.asarray(engine.generate(input512,mask512,prompt=params.prompt,negative_prompt=params.negative_prompt,
                seed=params.seed,steps=25,guidance_scale=params.guidance_scale,scheduler='ddim_v1'),np.float32)
            if generated.shape!=(512,512,3) or not np.isfinite(generated).all() or np.any(generated<0) or np.any(generated>1):raise ValueError('MODEL_OUTPUT_INVALID')
            model_output=cv2.resize(generated,(x1-x0,y1-y0),interpolation=cv2.INTER_CUBIC).clip(0,1).astype(np.float32)
            restored,_=transfer_model_delta(roi,model_input,model_output,1.);ctx.progress(1,5)
            distance=cv2.distanceTransform(mask.astype(np.uint8),cv2.DIST_L2,3);weight=np.minimum(distance/params.feather_pixels,1).astype(np.float32);weight[protected[y0:y1,x0:x1]]=0
            with artifacts.scratch.reserve(h*w*12+16384) as reservation:
                path=reservation.path('generated-rgb','.npy');output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3))
                try:
                    for row in range(0,h,256):ctx.check_active();output[row:row+256]=source[row:row+256]
                    original=np.asarray(source[y0:y1,x0:x1],np.float32);output[y0:y1,x0:x1]=original+(restored-original)*weight[...,None]
                    output.flush();reservation.validate()
                finally:output._mmap.close()
                pixel_ref=artifacts.stage(path,reservation,'working_rgb','application/octet-stream');written=path.stat().st_size
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    ctx.progress(2,5);output_image=image.model_copy(update={'pixels':pixel_ref})
    report=GenerationReport(image=params.image,mask=params.mask.artifact,model_id=params.model_id,model_lock_id=params.model_lock_id,
        backend=backend,seed=params.seed,guidance_scale=params.guidance_scale,generated_pixels=int(np.count_nonzero(effective)),
        protected_pixels=int(np.count_nonzero(protected)),side=None,source_rect=[0,0,w,h])
    image_ref=artifacts.stage_contract(output_image,'working_image');ctx.progress(4,5);report_ref=artifacts.stage_contract(report,'generation_report');ctx.progress(5,5)
    return OperationResult(outputs=[pixel_ref,image_ref,report_ref],review_required=True,bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))

def generation_operations(config:ArtifactConfig,pool:ModelPool,backend:str)->list[Operation]:
    def generated(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image','generation_report'})) as artifacts:return execute(ctx,artifacts,pool,backend)
    return [Operation('image.generate','gpu',generated)]

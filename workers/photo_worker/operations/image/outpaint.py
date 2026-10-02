"""One-side deterministic SD1.5 outpaint operation with exact source restoration."""
from __future__ import annotations
from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.model_delta import transfer_model_delta,work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import Canvas,MatrixTransform,Rect,TransformChain,WorkingImage
from photo_worker.contracts.restoration import GenerationReport,OutpaintParams
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult

def execute(ctx:ExecutionContext,artifacts:TaskArtifacts,pool:ModelPool,backend:str)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!=backend or backend not in ('cpu','cuda','mps') or type(ram) is not int or ram<2147483648:raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(OutpaintParams)
        if params.model_lock_id!=pool.lock.lock_id:raise ModelError('MODEL_LOCK_MISMATCH')
        image=artifacts.contract(params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('OUTPAINT_IMAGE_INVALID')
        required=[image.pixels,*([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('OUTPAINT_INPUT_UNDECLARED')
        h,w=image.canvas.height,image.canvas.width
        ow=w+params.pixels if params.side in ('left','right') else w;oh=h+params.pixels if params.side in ('top','bottom') else h
        if ow*oh>200000000:raise ValueError('OUTPAINT_PIXEL_LIMIT')
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    dx=params.pixels if params.side=='left' else 0;dy=params.pixels if params.side=='top' else 0;source_rect=[dx,dy,w,h]
    ctx.progress(0,5)
    try:
        with pool.acquire(params.model_id,backend) as engine,ExitStack() as stack:
            local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'));source=stack.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
            alpha=None
            if image.alpha:
                alocal=stack.enter_context(artifacts.input(image.alpha,suffix='.npy'));alpha=stack.enter_context(read_array(alocal.path,(h,w),'uint16',ctx.check_active))
            initial=np.empty((oh,ow,3),np.float32);initial[dy:dy+h,dx:dx+w]=source
            if params.side=='left':initial[:,:dx]=source[:,:1]
            elif params.side=='right':initial[:,w:]=source[:,-1:]
            elif params.side=='top':initial[:dy]=source[:1]
            else:initial[h:]=source[-1:]
            mask=np.ones((oh,ow),bool);mask[dy:dy+h,dx:dx+w]=False
            model_input,_=work_to_model(initial);input512=cv2.resize(model_input,(512,512),interpolation=cv2.INTER_AREA if max(oh,ow)>512 else cv2.INTER_CUBIC)
            mask512=cv2.resize(mask.astype(np.uint8),(512,512),interpolation=cv2.INTER_NEAREST).astype(np.float32)
            generated=np.asarray(engine.generate(input512,mask512,prompt=params.prompt,negative_prompt=params.negative_prompt,
                seed=params.seed,steps=25,guidance_scale=params.guidance_scale,scheduler='ddim_v1'),np.float32)
            if generated.shape!=(512,512,3) or not np.isfinite(generated).all() or np.any(generated<0) or np.any(generated>1):raise ValueError('MODEL_OUTPUT_INVALID')
            model_output=cv2.resize(generated,(ow,oh),interpolation=cv2.INTER_CUBIC).clip(0,1).astype(np.float32)
            restored,_=transfer_model_delta(initial,model_input,model_output,1.);distance=cv2.distanceTransform(mask.astype(np.uint8),cv2.DIST_L2,3)
            weight=np.minimum(distance/params.feather_pixels,1).astype(np.float32);result=initial+(restored-initial)*weight[...,None]
            result[dy:dy+h,dx:dx+w]=source;ctx.progress(1,5)
            reserve=oh*ow*(12+(2 if alpha is not None else 0))+32768
            with artifacts.scratch.reserve(reserve) as reservation:
                path=reservation.path('outpaint-rgb','.npy');output=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(oh,ow,3));output[:]=result;output.flush();output._mmap.close()
                alpha_path=None;out_alpha=None
                if alpha is not None:
                    alpha_path=reservation.path('outpaint-alpha','.npy');out_alpha=np.lib.format.open_memmap(alpha_path,mode='w+',dtype='<u2',shape=(oh,ow));out_alpha[:]=65535;out_alpha[dy:dy+h,dx:dx+w]=alpha;out_alpha.flush();out_alpha._mmap.close()
                reservation.validate();pixel_ref=artifacts.stage(path,reservation,'working_rgb','application/octet-stream');alpha_ref=artifacts.stage(alpha_path,reservation,'working_alpha','application/octet-stream') if alpha_path else None
                written=path.stat().st_size+(alpha_path.stat().st_size if alpha_path else 0)
    except ModelError as exc:raise OperationFailure(str(exc)) from exc
    except ValueError as exc:raise OperationFailure('MODEL_OUTPUT_INVALID') from exc
    canvas=Canvas(width=ow,height=oh,orientation=1,space_id=f'out-{params.side}-{image.canvas.source_sha256[:12]}',source_sha256=image.canvas.source_sha256)
    matrix=[1.,0.,float(dx),0.,1.,float(dy),0.,0.,1.]
    step=MatrixTransform(kind='matrix',from_canvas=image.canvas,to_canvas=canvas,invertibility='partial',valid_region=Rect(x=dx,y=dy,width=w,height=h),forward_matrix=matrix)
    chain=TransformChain.create([*image.transform_chain.steps,step]);output_image=WorkingImage(pixels=pixel_ref,canvas=canvas,color=image.color,dtype='float32',alpha=alpha_ref,transform_chain=chain)
    report=GenerationReport(image=params.image,mask=None,model_id=params.model_id,model_lock_id=params.model_lock_id,backend=backend,
        seed=params.seed,guidance_scale=params.guidance_scale,generated_pixels=int(np.count_nonzero(mask)),protected_pixels=w*h,
        side=params.side,source_rect=source_rect)
    ctx.progress(2,5);image_ref=artifacts.stage_contract(output_image,'working_image');ctx.progress(4,5);report_ref=artifacts.stage_contract(report,'generation_report');ctx.progress(5,5)
    return OperationResult(outputs=[pixel_ref,*([alpha_ref] if alpha_ref else []),image_ref,report_ref],review_required=True,
        bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))

def outpaint_operations(config:ArtifactConfig,pool:ModelPool,backend:str)->list[Operation]:
    def outpainted(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','generation_report'})) as artifacts:return execute(ctx,artifacts,pool,backend)
    return [Operation('image.outpaint','gpu',outpainted)]

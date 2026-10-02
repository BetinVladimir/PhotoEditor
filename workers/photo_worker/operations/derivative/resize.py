from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.color import to_encoded,to_linear
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.derivative import ResizeParams,DerivativeReport
from photo_worker.contracts.images import Canvas,Rect,ResizeTransform,TransformChain,WorkingImage
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from .common import open_image,stage_arrays,validate_image


def _dimensions(w,h,p):
    scale=p.long_edge/max(w,h) if p.long_edge is not None else min(p.width/w,p.height/h)
    if not p.allow_upscale:scale=min(1.,scale)
    return max(1,round(w*scale)),max(1,round(h*scale))


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    try:
        p=artifacts.params(ResizeParams);image,required=validate_image(ctx,artifacts,p.image)
        if ctx.lease.execution_profile.get('backend')!='cpu' or list(ctx.lease.command.inputs)!=required:raise ValueError('Resize binding mismatch')
        w,h=_dimensions(image.canvas.width,image.canvas.height,p)
        if w*h>200000000:raise ValueError('OUTPUT_PIXELS_LIMIT')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,4)
    with ExitStack() as stack:
        rgb,alpha=open_image(stack,ctx,artifacts,image);same=(w,h)==(image.canvas.width,image.canvas.height)
        if same:out=np.array(rgb,copy=True);out_alpha=None if alpha is None else np.array(alpha,copy=True)
        else:
            interpolation=cv2.INTER_AREA if w<image.canvas.width or h<image.canvas.height else cv2.INTER_LANCZOS4
            out=to_encoded(cv2.resize(to_linear(np.asarray(rgb)),(w,h),interpolation=interpolation));out_alpha=None if alpha is None else np.clip(cv2.resize(alpha,(w,h),interpolation=interpolation),0,1)
        with artifacts.scratch.reserve(w*h*(14 if alpha is not None else 12)+8192) as reservation:
            pixel,alpha_ref,outputs,written=stage_arrays(artifacts,reservation,out,out_alpha)
    canvas=Canvas(width=w,height=h,orientation=1,space_id='resize:'+canonical_hash({'image':p.image.model_dump(mode='json'),'width':w,'height':h})[:32],source_sha256=image.canvas.source_sha256);step=ResizeTransform(kind='resize',from_canvas=image.canvas,to_canvas=canvas,invertibility='invertible',valid_region=Rect(x=0,y=0,width=w,height=h),pixel_centers='half_pixel');chain=TransformChain.create([*image.transform_chain.steps,step]);result_image=WorkingImage(pixels=pixel,canvas=canvas,color=image.color,dtype='float32',alpha=alpha_ref,transform_chain=chain);image_ref=artifacts.stage_contract(result_image,'working_image');report=DerivativeReport(image=p.image,operation='resize',applied=not same,width=w,height=h,max_channel_delta=0,warnings=['no_resize_required'] if same else []);report_ref=artifacts.stage_contract(report,'derivative_report');ctx.progress(4,4);return OperationResult(outputs=[*outputs,image_ref,report_ref],bytes_written=written+len(canonical_bytes(result_image))+len(canonical_bytes(report)))


def resize_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','derivative_report'})) as a:return execute(ctx,a)
    return [Operation('derivative.resize','cpu',run)]

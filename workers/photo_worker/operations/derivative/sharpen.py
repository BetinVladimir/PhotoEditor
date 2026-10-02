from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.color import LUMA,to_encoded,to_linear
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.derivative import SharpenParams,DerivativeReport
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from .common import open_image,stage_arrays,validate_image


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    try:
        p=artifacts.params(SharpenParams);image,required=validate_image(ctx,artifacts,p.image)
        if ctx.lease.execution_profile.get('backend')!='cpu' or list(ctx.lease.command.inputs)!=required:raise ValueError('Sharpen binding mismatch')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    with ExitStack() as stack:
        rgb,alpha=open_image(stack,ctx,artifacts,image)
        if p.strength==0:out=np.array(rgb,copy=True);maximum=0.
        else:
            linear=to_linear(np.asarray(rgb));luma=np.sum(linear*LUMA.astype(np.float32),axis=2);blur=cv2.GaussianBlur(luma,(0,0),p.radius,borderType=cv2.BORDER_REFLECT101);detail=luma-blur;detail=np.where(np.abs(detail)>=p.threshold,detail,0);detail=np.clip(detail*p.strength,-p.max_delta,p.max_delta);processed=np.clip(linear+detail[...,None],0,1);out=to_encoded(processed);maximum=float(np.max(np.abs(processed-linear)))
        h,w=out.shape[:2]
        with artifacts.scratch.reserve(w*h*(14 if alpha is not None else 12)+8192) as reservation:pixel,alpha_ref,outputs,written=stage_arrays(artifacts,reservation,out,alpha)
    result_image=image.model_copy(update={'pixels':pixel,'alpha':alpha_ref});image_ref=artifacts.stage_contract(result_image,'working_image');report=DerivativeReport(image=p.image,operation='sharpen',applied=p.strength>0,width=w,height=h,max_channel_delta=maximum,warnings=['zero_strength'] if p.strength==0 else []);report_ref=artifacts.stage_contract(report,'derivative_report');return OperationResult(outputs=[*outputs,image_ref,report_ref],bytes_written=written+len(canonical_bytes(result_image))+len(canonical_bytes(report)))


def sharpen_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','derivative_report'})) as a:return execute(ctx,a)
    return [Operation('derivative.sharpen','cpu',run)]

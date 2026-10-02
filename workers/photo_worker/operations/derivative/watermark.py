from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.color import to_encoded,to_linear
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.derivative import WatermarkParams,DerivativeReport
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from .common import open_image,stage_arrays,validate_image


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    try:
        p=artifacts.params(WatermarkParams);image,required=validate_image(ctx,artifacts,p.image);mark,mark_required=validate_image(ctx,artifacts,p.watermark)
        if ctx.lease.execution_profile.get('backend')!='cpu' or list(ctx.lease.command.inputs)!=required+mark_required or image.color!=mark.color:raise ValueError('Watermark binding mismatch')
        if mark.alpha is None:raise ValueError('Watermark requires alpha')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    with ExitStack() as stack:
        rgb,alpha=open_image(stack,ctx,artifacts,image);mrgb,ma=open_image(stack,ctx,artifacts,mark);h,w=rgb.shape[:2];scale=min(1.,w*p.max_width_fraction/mark.canvas.width);mw=max(1,round(mark.canvas.width*scale));mh=max(1,round(mark.canvas.height*scale));mrgb=cv2.resize(to_linear(np.asarray(mrgb)),(mw,mh),interpolation=cv2.INTER_AREA);ma=cv2.resize(ma,(mw,mh),interpolation=cv2.INTER_AREA)*p.opacity
        mx=round(p.margin_x*w);my=round(p.margin_y*h);positions={'top_left':(mx,my),'top_right':(w-mw-mx,my),'bottom_left':(mx,h-mh-my),'bottom_right':(w-mw-mx,h-mh-my),'center':((w-mw)//2,(h-mh)//2)};x,y=positions[p.anchor]
        if x<0 or y<0 or x+mw>w or y+mh>h:raise OperationFailure('INPUT_INVALID')
        linear=to_linear(np.asarray(rgb));out=np.array(linear,copy=True);region=out[y:y+mh,x:x+mw];region[:]=region*(1-ma[...,None])+mrgb*ma[...,None];encoded=to_encoded(out);maximum=float(np.max(np.abs(out-linear)))
        with artifacts.scratch.reserve(w*h*(14 if alpha is not None else 12)+8192) as reservation:pixel,alpha_ref,outputs,written=stage_arrays(artifacts,reservation,encoded,alpha)
    result_image=image.model_copy(update={'pixels':pixel,'alpha':alpha_ref});image_ref=artifacts.stage_contract(result_image,'working_image');report=DerivativeReport(image=p.image,operation='watermark',applied=p.opacity>0,width=w,height=h,max_channel_delta=maximum,warnings=['zero_opacity'] if p.opacity==0 else []);report_ref=artifacts.stage_contract(report,'derivative_report');return OperationResult(outputs=[*outputs,image_ref,report_ref],bytes_written=written+len(canonical_bytes(result_image))+len(canonical_bytes(report)))


def watermark_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','derivative_report'})) as a:return execute(ctx,a)
    return [Operation('derivative.watermark','cpu',run)]

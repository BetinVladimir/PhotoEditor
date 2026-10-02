from contextlib import ExitStack
import cv2
import numpy as np
from photo_worker.algorithms.color import to_encoded,to_linear
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.contracts.composition import CompositionPlan,CropParams,CropReport
from photo_worker.contracts.images import Canvas,MatrixTransform,Rect,TransformChain,WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(CropParams);image=artifacts.contract(p.image,WorkingImage);plan=artifacts.contract(p.plan,CompositionPlan)
        if plan.image!=p.image or plan.source_width!=image.canvas.width or plan.source_height!=image.canvas.height or plan.disposition=='outpaint':raise ValueError('Plan mismatch or not applicable')
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or plan.output_width*plan.output_height>200000000:raise ValueError('Invalid image')
        required=[image.pixels,*([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('Undeclared image artifact')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width;oh,ow=plan.output_height,plan.output_width;matrix=np.asarray(plan.forward_matrix,np.float64).reshape(3,3)
    with ExitStack() as stack:
        rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));alpha=None if image.alpha is None else stack.enter_context(read_array(stack.enter_context(artifacts.input(image.alpha,suffix='.npy')).path,(h,w),'uint16',ctx.check_active))
        reserve=ow*oh*(12+(2 if alpha is not None else 0))+65536
        with artifacts.scratch.reserve(reserve) as reservation:
            path=reservation.path('crop-rgb','.npy');out=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(oh,ow,3));out[:]=to_encoded(cv2.warpPerspective(to_linear(np.asarray(rgb)),matrix,(ow,oh),flags=cv2.INTER_LANCZOS4,borderMode=cv2.BORDER_REFLECT_101)).clip(0,1);out.flush();out._mmap.close()
            alpha_path=None
            if alpha is not None:
                alpha_path=reservation.path('crop-alpha','.npy');aout=np.lib.format.open_memmap(alpha_path,mode='w+',dtype='<u2',shape=(oh,ow));aout[:]=np.rint(np.clip(cv2.warpPerspective(np.asarray(alpha,np.float32),matrix,(ow,oh),flags=cv2.INTER_LANCZOS4,borderMode=cv2.BORDER_REFLECT_101),0,65535)).astype(np.uint16);aout.flush();aout._mmap.close()
            reservation.validate();pixel_ref=artifacts.stage(path,reservation,'working_rgb','application/octet-stream');alpha_ref=artifacts.stage(alpha_path,reservation,'working_alpha','application/octet-stream') if alpha_path else None;written=path.stat().st_size+(alpha_path.stat().st_size if alpha_path else 0)
    canvas=Canvas(width=ow,height=oh,orientation=1,space_id='crop:'+canonical_hash({'plan':p.plan.model_dump(mode='json')})[:32],source_sha256=image.canvas.source_sha256);step=MatrixTransform(kind='matrix',from_canvas=image.canvas,to_canvas=canvas,invertibility='invertible',valid_region=Rect(x=0,y=0,width=ow,height=oh),forward_matrix=plan.forward_matrix);chain=TransformChain.create([*image.transform_chain.steps,step]);output=WorkingImage(pixels=pixel_ref,canvas=canvas,color=image.color,dtype='float32',alpha=alpha_ref,transform_chain=chain);image_ref=artifacts.stage_contract(output,'working_image')
    report=CropReport(image=p.image,plan=p.plan,applied=True,output_width=ow,output_height=oh,transform_count=1,interpolation=p.interpolation,alpha_preserved=alpha is not None,review_reasons=plan.reasons);report_ref=artifacts.stage_contract(report,'crop_report')
    return OperationResult(outputs=[pixel_ref,*([alpha_ref] if alpha_ref else []),image_ref,report_ref],review_required=plan.disposition=='review',bytes_written=written+len(canonical_bytes(output))+len(canonical_bytes(report)))


def crop_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','crop_report'})) as a:return execute(ctx,a)
    return [Operation('geometry.crop','cpu',run)]

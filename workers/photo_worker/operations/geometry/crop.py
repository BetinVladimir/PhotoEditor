from contextlib import ExitStack
from .prepare import prepare_crop,crop_geometry,crop_provider_peak
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.composition import CompositionPlan,CropParams,CropReport
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912:raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(CropParams);image=artifacts.contract(p.image,WorkingImage);plan=artifacts.contract(p.plan,CompositionPlan)
        canvas,chain=crop_geometry(p.image,image,p.plan,plan)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1' or plan.output_width*plan.output_height>200000000:raise ValueError('Invalid image')
        required=[image.pixels,*([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):raise ValueError('Undeclared image artifact')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    peak=crop_provider_peak(p.image,image,p.plan,plan)
    output_pixels=plan.output_width*plan.output_height
    source_pixels=image.canvas.width*image.canvas.height
    bpp=14 if image.alpha is not None else 12
    required=dict(ram_bytes=max(3<<30,peak['ram_bytes']),
        scratch_bytes=peak['scratch_bytes']+bpp*source_pixels+8192,
        output_bytes=bpp*output_pixels+2*1048576,vram_bytes=0)
    if type(estimate.get('pixel_count')) is not int or estimate['pixel_count']!=output_pixels:
        raise OperationFailure('CONFIG_INVALID')
    for name, minimum in required.items():
        value=estimate.get(name)
        if type(value) is not int or not 0<=value<=2**53-1:
            raise OperationFailure('CONFIG_INVALID')
        if value<minimum:
            raise OperationFailure({'ram_bytes':'RAM_ADMISSION_EXCEEDED',
                'scratch_bytes':'SCRATCH_ADMISSION_EXCEEDED',
                'output_bytes':'OUTPUT_ADMISSION_EXCEEDED','vram_bytes':'VRAM_ADMISSION_EXCEEDED'}[name])
    h,w=image.canvas.height,image.canvas.width;oh,ow=plan.output_height,plan.output_width
    with ExitStack() as stack:
        rgb=stack.enter_context(read_array(stack.enter_context(artifacts.input(image.pixels,suffix='.npy')).path,(h,w,3),'float32',ctx.check_active,unit_interval=True));alpha=None if image.alpha is None else stack.enter_context(read_array(stack.enter_context(artifacts.input(image.alpha,suffix='.npy')).path,(h,w),'uint16',ctx.check_active))
        with prepare_crop(rgb,alpha,plan,artifacts.scratch,check_active=ctx.check_active) as prepared:
            path,alpha_path,reservation=prepared.rgb,prepared.alpha,prepared.reservation
            reservation.validate();pixel_ref=artifacts.stage(path,reservation,'working_rgb','application/octet-stream');alpha_ref=artifacts.stage(alpha_path,reservation,'working_alpha','application/octet-stream') if alpha_path else None;written=path.stat().st_size+(alpha_path.stat().st_size if alpha_path else 0)
    output=WorkingImage(pixels=pixel_ref,canvas=canvas,color=image.color,dtype='float32',alpha=alpha_ref,transform_chain=chain);image_ref=artifacts.stage_contract(output,'working_image')
    report=CropReport(image=p.image,plan=p.plan,applied=True,output_width=ow,output_height=oh,transform_count=1,interpolation=p.interpolation,alpha_preserved=alpha is not None,review_reasons=plan.reasons);report_ref=artifacts.stage_contract(report,'crop_report')
    return OperationResult(outputs=[pixel_ref,*([alpha_ref] if alpha_ref else []),image_ref,report_ref],review_required=plan.disposition=='review',bytes_written=written+len(canonical_bytes(output))+len(canonical_bytes(report)))


def crop_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_alpha','working_image','crop_report'})) as a:return execute(ctx,a)
    return [Operation('geometry.crop','cpu',run)]

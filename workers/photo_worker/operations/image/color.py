"""Global color only; image.local performs final aligned strength blending."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.color import apply_global,trilinear,ColorDiagnostics
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.image_color import ColorParams,ColorReport,ClippingStage
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.resolved import ResolvedStyle
from photo_worker.runtime.arrays import read_array,read_lut
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    tile=profile.get('tile_size',512)
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<536870912 or type(tile) is not int or tile not in (256,512,1024):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(ColorParams)
        image=artifacts.contract(params.image,WorkingImage)
        resolved=artifacts.contract(params.resolved,ResolvedStyle)
        if resolved.mode=='skip':raise OperationFailure('STYLE_OOD_SKIP')
        source=image if resolved.params.image==params.image else artifacts.contract(resolved.params.image,WorkingImage)
        if (image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
            or image.color!=source.color or image.canvas.source_sha256!=source.canvas.source_sha256):
            raise ValueError('COLOR_SOURCE_MISMATCH')
        if image.alpha is not None and image.alpha not in ctx.lease.command.inputs:raise ValueError('ALPHA_INPUT_UNDECLARED')
        h,w=image.canvas.height,image.canvas.width
        if h*w>200000000:raise ValueError('COLOR_PIXEL_LIMIT')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    total=((h+tile-1)//tile)*((w+tile-1)//tile)+2
    ctx.progress(0,total)
    with ExitStack() as inputs:
        local=inputs.enter_context(artifacts.input(image.pixels,suffix='.npy'))
        rgb=inputs.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True))
        lut=None
        if resolved.absolute_lut is not None:
            local=inputs.enter_context(artifacts.input(resolved.absolute_lut.artifact,suffix='.npy'))
            lut=inputs.enter_context(read_lut(local.path,resolved.absolute_lut.size,ctx.check_active))
        sums={};done=0
        with artifacts.scratch.reserve(h*w*12+8192) as reservation:
            path=reservation.path('colored-rgb','.npy')
            target=np.lib.format.open_memmap(path,mode='w+',dtype='<f4',shape=(h,w,3))
            try:
                for y in range(0,h,tile):
                    for x in range(0,w,tile):
                        ctx.check_active();block=rgb[y:y+tile,x:x+tile]
                        diagnostic=ColorDiagnostics()
                        if resolved.fixed is not None:output=apply_global(block,resolved.fixed,diagnostic)
                        else:
                            output=trilinear(lut,block)
                            diagnostic.record('absolute_lut',output)
                            output=np.clip(output,0,1).astype(np.float32)
                        target[y:y+tile,x:x+tile]=output
                        for stage,values in diagnostic.clipping.items():
                            counts=sums.setdefault(stage,[0,0])
                            counts[0]+=round(values['below_zero']*block.size)
                            counts[1]+=round(values['above_one']*block.size)
                        done+=1;ctx.progress(done,total)
                target.flush();reservation.validate()
            finally:target._mmap.close()
            pixel_ref=artifacts.stage(path,reservation,'working_rgb','application/octet-stream')
            written=path.stat().st_size
    output_image=WorkingImage(pixels=pixel_ref,canvas=image.canvas,color=image.color,dtype='float32',alpha=image.alpha,transform_chain=image.transform_chain)
    report=ColorReport(image=params.image,resolved=params.resolved,pixel_count=h*w,
        clipping=[ClippingStage(stage=stage,below_zero=counts[0]/(h*w*3),above_one=counts[1]/(h*w*3)) for stage,counts in sums.items()])
    image_ref=artifacts.stage_contract(output_image,'working_image');ctx.progress(total-1,total)
    report_ref=artifacts.stage_contract(report,'color_report');ctx.progress(total,total)
    return OperationResult(outputs=[pixel_ref,image_ref,report_ref],review_required=resolved.needs_review,
        bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))


def image_color_operations(config:ArtifactConfig)->list[Operation]:
    def colored(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'working_rgb','working_image','color_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('image.color','cpu',colored)]

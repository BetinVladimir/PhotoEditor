from contextlib import ExitStack
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.mask_transform import MaskTransformParams,TransformedMask
from photo_worker.contracts.images import WorkingImage,MaskRef,DenseWarpTransform
from photo_worker.runtime.arrays import read_array,read_inverse_field
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from .transform_prepare import prepare_transform


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    tile=profile.get('tile_size',512)
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824 or type(tile) is not int or tile not in (256,512,1024):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(MaskTransformParams)
        target=artifacts.contract(params.target,WorkingImage)
        chain=params.validate_target(target)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,2)
    with ExitStack() as inputs:
        def local(ref):return inputs.enter_context(artifacts.input(ref,suffix='.npy')).path
        sh,sw=params.mask.canvas.height,params.mask.canvas.width
        h,w=target.canvas.height,target.canvas.width
        labels=params.dtype=='uint16'
        source=inputs.enter_context(read_array(local(params.mask.artifact),(sh,sw),params.dtype,ctx.check_active,unit_interval=not labels))
        field=None
        if isinstance(params.step,DenseWarpTransform):
            field=inputs.enter_context(read_inverse_field(local(params.step.inverse_field),(h,w),ctx.check_active))
        guide=None if labels else inputs.enter_context(read_array(local(target.pixels),(h,w,3),'float32',ctx.check_active,unit_interval=True))
        with prepare_transform(source,params.step,artifacts.scratch,guide=guide,labels=labels,inverse_field=field,tile=tile,
                               max_work_pixels=min(4194304,ram//256),check_active=ctx.check_active) as files:
            ctx.progress(1,2)
            pixels=artifacts.stage(files.array,files.arrays,'transformed_mask','application/octet-stream')
            png=artifacts.stage(files.png,files.pngs,'transformed_mask_png','image/png')
            written=files.array.stat().st_size+files.png.stat().st_size
            radius=files.radius
    mask=MaskRef(artifact=pixels,canvas=target.canvas,transform_chain=chain,object_id=params.mask.object_id,
                 selector=params.mask.selector,confidence=params.mask.confidence,model_lock_id=params.mask.model_lock_id,protected=params.mask.protected)
    result=TransformedMask(params=params,mask=mask,png=png,dtype=params.dtype,guided_radius=radius)
    manifest=artifacts.stage_contract(result,'mask_transform_result');ctx.progress(2,2)
    return OperationResult(outputs=[pixels,png,manifest],bytes_written=written+len(canonical_bytes(result)))


def mask_transform_operations(config:ArtifactConfig)->list[Operation]:
    def transformed(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'transformed_mask','transformed_mask_png','mask_transform_result'})) as artifacts:
            return execute(ctx,artifacts)
    return [Operation('mask.transform','cpu',transformed)]

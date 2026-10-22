from contextlib import ExitStack
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.mask_transform import MaskTransformParams,TransformedMask,TransformedRegion
from photo_worker.contracts.images import WorkingImage,MaskRef,DenseWarpTransform
from photo_worker.runtime.arrays import read_array,read_inverse_field
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from .transform_prepare import prepare_transform
from .transform_publish import stage_transform,finish_transform,PublishedTransform


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
            published=stage_transform(artifacts,files,params.model_copy(update={'regions':[]}),target,check_active=ctx.check_active)
    if params.regions:
        regional=[];cache={};outputs=list(published.outputs);written=published.bytes_written-len(canonical_bytes(published.result))
        for region in params.regions:
            ctx.check_active()
            key=(region.mask.artifact.artifact_id,region.mask.artifact.sha256)
            if key not in cache:
                single=params.model_copy(update={'mask':region.mask,'regions':[]})
                with ExitStack() as regional_inputs:
                    def regional_array(ref,shape):
                        item=regional_inputs.enter_context(artifacts.input(ref,suffix='.npy'))
                        return regional_inputs.enter_context(read_array(item.path,shape,'float32',ctx.check_active,unit_interval=True))
                    source=regional_array(region.mask.artifact,(sh,sw))
                    guide=regional_array(target.pixels,(h,w,3))
                    field=None
                    if isinstance(params.step,DenseWarpTransform):
                        item=regional_inputs.enter_context(artifacts.input(params.step.inverse_field,suffix='.npy'))
                        field=regional_inputs.enter_context(read_inverse_field(item.path,(h,w),ctx.check_active))
                    with prepare_transform(source,params.step,artifacts.scratch,guide=guide,labels=False,inverse_field=field,tile=tile,
                                           max_work_pixels=min(4194304,ram//256),check_active=ctx.check_active) as files:
                        transformed=stage_transform(artifacts,files,single,target,check_active=ctx.check_active)
                cache[key]=transformed
                outputs.extend(transformed.outputs)
                written+=transformed.bytes_written-len(canonical_bytes(transformed.result))
            transformed=cache[key].result
            mask=region.mask.model_copy(update={'artifact':transformed.mask.artifact,'canvas':target.canvas,'transform_chain':chain})
            regional.append(TransformedRegion(region_id=region.region_id,purpose=region.purpose,mask=mask,png=transformed.png,guided_radius=transformed.guided_radius))
        root=published.result.model_copy(update={'params':params,'regions':regional})
        published=PublishedTransform(root,tuple(outputs),written+len(canonical_bytes(root)))
    published=finish_transform(artifacts,published,check_active=ctx.check_active)
    ctx.progress(2,2)
    return OperationResult(outputs=list(published.outputs),bytes_written=published.bytes_written)


def mask_transform_operations(config:ArtifactConfig)->list[Operation]:
    def transformed(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'transformed_mask','transformed_mask_png','mask_transform_result'})) as artifacts:
            return execute(ctx,artifacts)
    return [Operation('mask.transform','cpu',transformed)]

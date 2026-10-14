from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.manual_mask import ManualMask,MaskRasterParams,RasterizedMask
from photo_worker.contracts.images import WorkingImage,MaskRef
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure
from .prepare import prepare_raster
from .verify import verify_raster
from photo_worker.runtime.arrays import read_array


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    tile=profile.get('tile_size',512)
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<268435456 or type(tile) is not int or tile not in (256,512,1024):
        raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(MaskRasterParams)
        image=artifacts.contract(params.image,WorkingImage)
        vectors=params.inline_vectors if params.inline_vectors is not None else artifacts.contract(params.vectors,ManualMask)
        if image.canvas.source_sha256!=vectors.source_sha256:
            raise OperationFailure('OVERRIDE_SOURCE_MISMATCH')
        if image.canvas!=vectors.canvas or image.transform_chain.steps:
            raise ValueError('Manual masks must rasterize in oriented source coordinates')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(0,2)
    with prepare_raster(vectors,image.canvas.source_sha256,artifacts.scratch,tile=tile,check_active=ctx.check_active) as files:
        ctx.progress(1,2)
        try:
            shape=(image.canvas.height,image.canvas.width)
            with read_array(files.target,shape,'float32',ctx.check_active,unit_interval=True) as actual_target, read_array(files.protected,shape,'float32',ctx.check_active,unit_interval=True) as actual_protected:
                verified_nonzero=verify_raster(vectors,image.canvas.source_sha256,actual_target,actual_protected,tile=tile,check_active=ctx.check_active)
            if verified_nonzero!=files.protected_nonzero:raise ValueError('RASTER_PROTECTION_FLAG_MISMATCH')
        except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
        target=artifacts.stage(files.target,files.arrays,'mask_target','application/octet-stream')
        protected=artifacts.stage(files.protected,files.arrays,'mask_protected','application/octet-stream')
        target_png=artifacts.stage(files.target_png,files.pngs,'mask_target_png','image/png')
        protected_png=artifacts.stage(files.protected_png,files.pngs,'mask_protected_png','image/png')
        written=sum(p.stat().st_size for p in (files.target,files.protected,files.target_png,files.protected_png))
        protected_nonzero=files.protected_nonzero
    common=dict(canvas=image.canvas,transform_chain=image.transform_chain,object_id=params.object_id,
                selector='manual',confidence=1.,model_lock_id=None)
    # Inline vectors are immutably contained in the exact task params artifact.
    vector_ref=params.vectors if params.vectors is not None else ctx.lease.command.params
    result=RasterizedMask(image=params.image,vectors=vector_ref,
        target=MaskRef(artifact=target,protected=False,**common),protected=MaskRef(artifact=protected,protected=True,**common),
        target_png=target_png,protected_png=protected_png)
    manifest=artifacts.stage_contract(result,'rasterized_mask')
    target_ref=artifacts.stage_contract(result.target,'mask_target_ref')
    guard_ref=artifacts.stage_contract(result.protected,'mask_guard_ref')
    outputs=[target,protected,target_png,protected_png,manifest,target_ref,guard_ref]
    if protected_nonzero:
        # A standalone immutable MaskRef can be bound into EditingRecipeV3.
        # Empty protection never becomes learning evidence.
        outputs.append(artifacts.stage_contract(result.protected,'mask_protected_ref'))
    ctx.progress(2,2)
    return OperationResult(outputs=outputs,bytes_written=written+len(canonical_bytes(result))+len(canonical_bytes(result.target))+len(canonical_bytes(result.protected))+(len(canonical_bytes(result.protected)) if protected_nonzero else 0))


def mask_operations(config:ArtifactConfig)->list[Operation]:
    def rasterized(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'mask_target','mask_protected','mask_target_png','mask_protected_png','rasterized_mask','mask_target_ref','mask_guard_ref','mask_protected_ref'})) as artifacts:
            return execute(ctx,artifacts)
    return [Operation('mask.rasterize','cpu',rasterized)]

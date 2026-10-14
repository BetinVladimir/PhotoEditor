"""Task adapter for evidenced before/after similarity registration."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.registration import register_pair
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.pair import RegistrationParams,PairAlignment,validate_alignment_origin
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    estimate=ctx.lease.execution_profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if type(ram) is not int or ram<268435456:raise OperationFailure('CONFIG_INVALID')
    ctx.progress(0,4)
    try:
        params=artifacts.params(RegistrationParams)
        images=[artifacts.contract(ref,WorkingImage) for ref in (params.before,params.after)]
        for image in images:
            if min(image.canvas.width,image.canvas.height)<8 or image.canvas.width*image.canvas.height>200000000:
                raise ValueError('PAIR_CANVAS_INVALID')
            if image.alpha is not None or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':
                raise ValueError('PAIR_WORKING_IMAGE_INVALID')
        if images[0].color!=images[1].color:raise ValueError('PAIR_PROFILES_DIFFER')
        if params.background_mask is not None:
            mask=params.background_mask
            if mask.canvas!=images[0].canvas or mask.transform_chain!=images[0].transform_chain:
                raise ValueError('ALIGNMENT_MASK_CANVAS_INVALID')
        ctx.progress(1,4)
        with ExitStack() as stack:
            arrays=[]
            for image in images:
                local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'))
                arrays.append(stack.enter_context(read_array(local.path,(image.canvas.height,image.canvas.width,3),'float32',ctx.check_active)))
            background=None
            if params.background_mask is not None:
                local=stack.enter_context(artifacts.input(params.background_mask.artifact,suffix='.npy'))
                canvas=images[0].canvas
                background=stack.enter_context(read_array(local.path,(canvas.height,canvas.width),'float32',ctx.check_active,unit_interval=True))
            ctx.progress(2,4)
            result=register_pair(*arrays,seed=params.seed,crop_aware=params.mode=='crop_aware',
                manual_matrix=np.array(params.manual_transform).reshape(3,3) if params.manual_transform is not None else None,
                background_mask=background,check_active=ctx.check_active)
        manifest=PairAlignment(before=params.before,after=params.after,before_canvas=images[0].canvas,after_canvas=images[1].canvas,
            before_to_after=result.before_to_after.ravel().tolist(),overlap=result.overlap,median_reprojection=result.median_reprojection,
            inlier_count=result.inlier_count,confidence=result.confidence,crop_aware=result.crop_aware,warnings=list(result.warnings))
        validate_alignment_origin(params,manifest)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(3,4)
    output=artifacts.stage_contract(manifest,'alignment')
    ctx.progress(4,4)
    return OperationResult(outputs=[output],review_required=bool(result.warnings),bytes_written=len(canonical_bytes(manifest)))


def pair_operations(config:ArtifactConfig)->list[Operation]:
    def registered(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'alignment'})) as artifacts:
            return execute(ctx,artifacts)
    return [Operation('pair.register','cpu',registered)]

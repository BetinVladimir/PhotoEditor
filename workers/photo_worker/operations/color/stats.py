"""Stats20 task adapter with an owned exact-quantile disk workspace."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.statistics import compute_stats20_bounded
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage,Stats20
from photo_worker.contracts.statistics import StatsParams
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile
    estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<268435456:raise OperationFailure('CONFIG_INVALID')
    ctx.progress(0,3)
    try:
        params=artifacts.params(StatsParams)
        image=artifacts.contract(params.image,WorkingImage)
        if image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('STATS_WORKING_IMAGE_INVALID')
        if params.person.canvas!=image.canvas or params.person.transform_chain!=image.transform_chain:raise ValueError('STATS_MASK_CANVAS_INVALID')
        shape=(image.canvas.height,image.canvas.width)
        pixels=shape[0]*shape[1]
        if pixels>200000000:raise ValueError('STATS_PIXEL_LIMIT')
        with ExitStack() as inputs:
            def array(ref,shape,dtype,unit=False):
                local=inputs.enter_context(artifacts.input(ref,suffix='.npy'))
                return inputs.enter_context(read_array(local.path,shape,dtype,ctx.check_active,unit_interval=unit))
            rgb=array(image.pixels,(*shape,3),'float32',True)
            person=array(params.person.artifact,shape,'float32',True)
            alpha=array(image.alpha,shape,'uint16') if image.alpha is not None else None
            ctx.progress(1,3)
            with artifacts.scratch.reserve(pixels*8+8192) as reservation:
                path=reservation.path('stats-luma','.npy')
                workspace=np.lib.format.open_memmap(path,mode='w+',dtype='<f8',shape=(pixels,))
                try:
                    result=compute_stats20_bounded(rgb,person,workspace,alpha=alpha,encoding='gamma24',check_active=ctx.check_active)
                    workspace.flush();reservation.validate()
                finally:workspace._mmap.close()
        warnings=['person_mask_low_confidence'] if params.person.confidence<.75 else []
        manifest=Stats20(source_image=params.image,feature_version='stats20_v1',values=result.values.tolist(),coverage_bins=result.coverage_bins.tolist(),
            mask_confidence=params.person.confidence,warnings=warnings)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(2,3)
    output=artifacts.stage_contract(manifest,'stats')
    ctx.progress(3,3)
    return OperationResult(outputs=[output],review_required=bool(warnings),bytes_written=len(canonical_bytes(manifest)))


def stats_operations(config:ArtifactConfig)->list[Operation]:
    def measured(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'stats'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('color.stats','cpu',measured)]

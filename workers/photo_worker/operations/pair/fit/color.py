"""Single-pair color fitting task; never silently selects another backend."""
from contextlib import ExitStack
from dataclasses import asdict
import numpy as np
from photo_worker.algorithms.fitting import fit_color,FitOptions,_local_reliable
from photo_worker.algorithms.registration import PairAlignment as NumericAlignment
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.color_fit import ColorFitParams,ColorFit,FitSamplingEvidence,FitCheckpoint
from photo_worker.operations.pair.prepare import prepare_samples
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import TaskArtifacts,ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationResult,OperationFailure


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile
    backend=profile.get('backend')
    estimate=profile.get('resource_estimate')
    ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None
    if backend not in ('cpu','mps','cuda') or type(ram) is not int or ram<1073741824:
        raise OperationFailure('CONFIG_INVALID')
    if backend in ('mps','cuda'):
        vram=estimate.get('vram_bytes')
        if type(vram) is not int or vram<536870912:raise OperationFailure('CONFIG_INVALID')
    tile=profile.get('tile_size',512)
    if type(tile) is not int or tile not in (256,512,1024):raise OperationFailure('CONFIG_INVALID')
    options=FitOptions(device=backend)
    try:options.validate()
    except ValueError as exc:raise OperationFailure('CONFIG_INVALID') from exc
    ctx.progress(0,5)
    try:
        params=artifacts.params(ColorFitParams)
        before=artifacts.contract(params.before,WorkingImage)
        after=artifacts.contract(params.after,WorkingImage)
        alignment=artifacts.contract(params.alignment,PairAlignment)
        if (alignment.before!=params.before or alignment.after!=params.after
            or alignment.before_canvas!=before.canvas or alignment.after_canvas!=after.canvas):
            raise ValueError('ALIGNMENT_SOURCE_MISMATCH')
        for image in (before,after):
            if (image.alpha is not None or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1'
                or min(image.canvas.width,image.canvas.height)<8 or image.canvas.width*image.canvas.height>200000000):
                raise ValueError('FIT_WORKING_IMAGE_INVALID')
        if before.color!=after.color:raise ValueError('FIT_PROFILE_MISMATCH')
        if params.person is not None and (params.person.canvas!=after.canvas or params.person.transform_chain!=after.transform_chain):
            raise ValueError('FIT_MASK_CANVAS_INVALID')
        if params.ignore is not None and (params.ignore.canvas!=after.canvas or params.ignore.transform_chain!=after.transform_chain):
            raise ValueError('FIT_IGNORE_CANVAS_INVALID')
        ctx.progress(1,5)
        with ExitStack() as stack:
            def pixels(ref,shape):
                local=stack.enter_context(artifacts.input(ref,suffix='.npy'))
                return stack.enter_context(read_array(local.path,shape,'float32',ctx.check_active,unit_interval=True))
            source=pixels(before.pixels,(before.canvas.height,before.canvas.width,3))
            target=pixels(after.pixels,(after.canvas.height,after.canvas.width,3))
            person=pixels(params.person.artifact,(after.canvas.height,after.canvas.width)) if params.person is not None else None
            ignore=pixels(params.ignore.artifact,(after.canvas.height,after.canvas.width)) if params.ignore is not None else None
            numeric=NumericAlignment(np.array(alignment.before_to_after).reshape(3,3),alignment.overlap,alignment.median_reprojection,
                alignment.inlier_count,alignment.confidence,alignment.crop_aware,tuple(alignment.warnings))
            samples,evidence=prepare_samples(str(params.pair_id),source,target,numeric,artifacts.scratch,
                person=person,ignore=ignore,seed=params.seed,max_samples=params.samples,tile=tile,check_active=ctx.check_active)
        ctx.progress(2,5)
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    warnings=[]
    if alignment.warnings:warnings.append('alignment_review_required')
    reliable=params.person is not None and params.person.confidence>=.75 and _local_reliable([samples])
    if params.local_requested and not reliable:
        warnings.append('local_mask_unavailable' if params.person is None else 'local_mask_unreliable')
    options=FitOptions(device=backend,local_steps=200 if params.local_requested and reliable else 0)
    result=fit_color([samples],options,check_active=ctx.check_active)
    ctx.progress(3,5)
    manifest=ColorFit(params=params,global_color=result.global_color,local=result.local,holdout_loss=result.holdout_loss,
        median_delta_e=result.metrics['median_delta_e'],p95_delta_e=result.metrics['p95_delta_e'],
        initialization_index=result.initialization_index,optimizer_version=result.optimizer_version,backend=backend,
        sampling=FitSamplingEvidence(**{**asdict(evidence),'holdout_blocks':list(evidence.holdout_blocks)},
            train_count=len(samples.train_before),holdout_count=len(samples.holdout_before)),
        history=[FitCheckpoint(**entry) for entry in result.history],warnings=warnings)
    ctx.progress(4,5)
    output=artifacts.stage_contract(manifest,'color_fit')
    ctx.progress(5,5)
    return OperationResult(outputs=[output],review_required=bool(warnings),bytes_written=len(canonical_bytes(manifest)))


def color_fit_operations(config:ArtifactConfig)->list[Operation]:
    def fitted(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'color_fit'})) as artifacts:
            return execute(ctx,artifacts)
    return [Operation('pair.fit.color','gpu',fitted)]

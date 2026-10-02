from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.color import LUMA,to_linear
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.series import RegionStats,SeriesMeasureParams,SeriesStats
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(SeriesMeasureParams);image=artifacts.contract(p.image,WorkingImage);required=[p.image,image.pixels,*([image.alpha] if image.alpha else []),*[r.mask.artifact for r in p.regions]]
        if list(ctx.lease.command.inputs)!=required or image.dtype!='float32' or image.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('Series measure binding')
        for r in p.regions:
            if r.mask.canvas!=image.canvas or r.mask.transform_chain!=image.transform_chain or r.mask.protected:raise ValueError('Series region geometry')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    h,w=image.canvas.height,image.canvas.width;stats=[];warnings=[]
    with ExitStack() as stack:
        local=stack.enter_context(artifacts.input(image.pixels,suffix='.npy'));rgb=stack.enter_context(read_array(local.path,(h,w,3),'float32',ctx.check_active,unit_interval=True));linear=to_linear(np.asarray(rgb));clip=float(np.mean((rgb<=1/65535)|(rgb>=1-1/65535)))
        for region in p.regions:
            local=stack.enter_context(artifacts.input(region.mask.artifact,suffix='.npy'));mask=stack.enter_context(read_array(local.path,(h,w),'float32',ctx.check_active,unit_interval=True));weight=float(np.sum(mask));coverage=weight/(h*w)
            mean=np.sum(linear*mask[...,None],axis=(0,1),dtype=np.float64)/max(weight,1e-12);confidence=min(region.mask.confidence,min(1.,coverage/.01)) if weight else 0.;stats.append(RegionStats(region_id=region.region_id,mean_rgb=np.clip(mean,0,1).tolist(),mean_y=float(np.dot(mean,LUMA)),coverage=coverage,confidence=confidence))
            if coverage<.01 and 'low_region_coverage' not in warnings:warnings.append('low_region_coverage')
            if region.mask.confidence<.75 and 'low_mask_confidence' not in warnings:warnings.append('low_mask_confidence')
    result=SeriesStats(item_id=p.item_id,image=p.image,regions=stats,clipping_fraction=clip,warnings=warnings);ref=artifacts.stage_contract(result,'series_stats');return OperationResult(outputs=[ref],review_required=bool(warnings),bytes_written=len(canonical_bytes(result)))


def measure_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'series_stats'})) as a:return execute(ctx,a)
    return [Operation('series.measure','cpu',run)]

import numpy as np
from scipy.optimize import least_squares
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.series import ItemCorrection,SeriesCorrections,SeriesParams,SeriesStats
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult


def _vector(stats,ids):
    by={r.region_id:r for r in stats.regions};return np.concatenate([np.log(np.maximum(by[i].mean_rgb,1e-6)) for i in ids])


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    if ctx.lease.execution_profile.get('backend')!='cpu':raise OperationFailure('CONFIG_INVALID')
    try:
        p=artifacts.params(SeriesParams);values=[artifacts.contract(r,SeriesStats) for r in p.stats]
        if len({v.item_id for v in values})!=len(values):raise ValueError('Duplicate series item')
        by={v.item_id:v for v in values}
        if p.reference_item_id is not None and p.reference_item_id not in by:raise ValueError('Unknown series reference')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    common=sorted(set.intersection(*({r.region_id for r in v.regions} for v in values)))
    if p.reference_item_id is None:
        candidates=[v for v in values if min(r.confidence for r in v.regions)>=p.min_confidence]
        if not candidates:candidates=values
        vectors={v.item_id:_vector(v,common) if common else np.zeros(1) for v in candidates};reference=min(candidates,key=lambda v:(sum(np.linalg.norm(vectors[v.item_id]-vectors[x.item_id]) for x in candidates),str(v.item_id)))
    else:reference=by[p.reference_item_id]
    corrections=[];skipped=False
    for value in sorted(values,key=lambda v:str(v.item_id)):
        if value.item_id==reference.item_id:corrections.append(ItemCorrection(item_id=value.item_id,raw_ev=0,raw_gains=[1.,1.,1.],applied_ev=0,applied_gains=[1.,1.,1.],confidence=1,accepted=True,reason='reference'));continue
        if not common:
            corrections.append(ItemCorrection(item_id=value.item_id,raw_ev=0,raw_gains=[1.,1.,1.],applied_ev=0,applied_gains=[1.,1.,1.],confidence=0,accepted=False,reason='no_common_regions'));skipped=True;continue
        left={r.region_id:r for r in value.regions};right={r.region_id:r for r in reference.regions};confidence=min(min(left[i].confidence,right[i].confidence) for i in common)
        src=np.asarray([left[i].mean_rgb for i in common]);dst=np.asarray([right[i].mean_rgb for i in common]);scale=np.maximum(dst,.05)
        def residual(x):return np.r_[((src*(2**x[0])*np.exp(x[1:])-dst)/scale).reshape(-1),x]
        fit=least_squares(residual,np.zeros(4),loss='soft_l1',max_nfev=200);raw_ev=float(fit.x[0]);raw_g=np.exp(fit.x[1:]);raw_g/=np.exp(np.mean(np.log(raw_g)));applied_ev=float(np.clip(raw_ev,-p.ev_limit,p.ev_limit));applied_g=np.clip(raw_g,p.gain_min,p.gain_max);pred=src*(2**applied_ev)*applied_g;before=float(np.mean(((src-dst)/scale)**2));after=float(np.mean(((pred-dst)/scale)**2));clip_delta=float(np.mean((pred<0)|(pred>1)))-value.clipping_fraction;reason='applied';accepted=True
        if confidence<p.min_confidence:reason='low_confidence';accepted=False
        elif after>=before:reason='residual_not_improved';accepted=False
        elif clip_delta>.005:reason='clipping_regression';accepted=False
        if not accepted:applied_ev=0.;applied_g=np.ones(3);skipped=True
        corrections.append(ItemCorrection(item_id=value.item_id,raw_ev=max(-10.,min(10.,raw_ev)),raw_gains=np.clip(raw_g,.01,100).tolist(),applied_ev=applied_ev,applied_gains=applied_g.tolist(),confidence=confidence,accepted=accepted,reason=reason))
    warnings=[]
    if not common:warnings.append('no_common_regions')
    if skipped:warnings.append('items_skipped')
    result=SeriesCorrections(scene_id=p.scene_id,reference_item_id=reference.item_id,common_region_ids=common,items=corrections,warnings=warnings);ref=artifacts.stage_contract(result,'series_corrections');return OperationResult(outputs=[ref],review_required=bool(warnings),bytes_written=len(canonical_bytes(result)))


def balance_operations(config:ArtifactConfig):
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'series_corrections'})) as a:return execute(ctx,a)
    return [Operation('series.balance','cpu',run)]

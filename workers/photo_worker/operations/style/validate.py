"""Independent held-out validation for an immutable StyleV2 candidate."""
from contextlib import ExitStack
import numpy as np
from photo_worker.algorithms.color import apply_global,apply_local,rgb_to_lab_d50
from photo_worker.algorithms.fitting import identity_color
from photo_worker.algorithms.registration import PairAlignment as NumericAlignment
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.pair import PairAlignment
from photo_worker.contracts.style import MetricBlock,Quality,StyleV2
from photo_worker.contracts.style_validation import StyleValidateParams,StyleQualityReport
from photo_worker.operations.pair.prepare import prepare_samples
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig,TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext,Operation,OperationFailure,OperationResult
from skimage.color import deltaE_ciede2000


def _ssim(left:np.ndarray,right:np.ndarray)->float:
    # Global RGB SSIM on an independently sampled population; finite and stable for flat patches.
    x=np.asarray(left,np.float64);y=np.asarray(right,np.float64);mx=x.mean(0);my=y.mean(0);vx=x.var(0);vy=y.var(0);cov=((x-mx)*(y-my)).mean(0)
    return float(np.mean(((2*mx*my+.01**2)*(2*cov+.03**2))/((mx*mx+my*my+.01**2)*(vx+vy+.03**2))))


def _metrics(style:StyleV2,samples,local:bool):
    per=[];count=0
    for pair in samples:
        output=apply_global(pair.holdout_before,style.global_)
        if local and style.local.enabled:output=apply_local(output,pair.holdout_person,style.local)
        delta=deltaE_ciede2000(rgb_to_lab_d50(output),rgb_to_lab_d50(pair.holdout_after));count+=len(delta)
        per.append((float(np.median(delta)),float(np.percentile(delta,95)),_ssim(output,pair.holdout_after)))
    return MetricBlock(median_delta_e=float(np.mean([v[0] for v in per])),p95_delta_e=float(np.mean([v[1] for v in per])),ssim=float(np.mean([v[2] for v in per])),sample_count=count)


def execute(ctx:ExecutionContext,artifacts:TaskArtifacts)->OperationResult:
    profile=ctx.lease.execution_profile;estimate=profile.get('resource_estimate');ram=estimate.get('ram_bytes') if isinstance(estimate,dict) else None;tile=profile.get('tile_size',512)
    if profile.get('backend')!='cpu' or type(ram) is not int or ram<1073741824 or tile not in (256,512,1024):raise OperationFailure('CONFIG_INVALID')
    try:
        params=artifacts.params(StyleValidateParams);style=artifacts.contract(params.style,StyleV2);samples=[];local_missing=False
        if style.quality.status=='example':raise ValueError('Example style cannot be validated')
        for index,item in enumerate(params.holdout):
            before=artifacts.contract(item.before,WorkingImage);after=artifacts.contract(item.after,WorkingImage);alignment=artifacts.contract(item.alignment,PairAlignment)
            if alignment.before!=item.before or alignment.after!=item.after or alignment.before_canvas!=before.canvas or alignment.after_canvas!=after.canvas:raise ValueError('Validation alignment mismatch')
            if before.dtype!='float32' or after.dtype!='float32' or before.alpha is not None or after.alpha is not None or before.color!=after.color or before.color.profile_id!='work_rec2020_gamma24_v1':raise ValueError('Validation image mismatch')
            if item.person is not None and (item.person.canvas!=after.canvas or item.person.transform_chain!=after.transform_chain):raise ValueError('Validation person geometry mismatch')
            if item.ignore is not None and (item.ignore.canvas!=after.canvas or item.ignore.transform_chain!=after.transform_chain):raise ValueError('Validation ignore geometry mismatch')
            if style.local.enabled and item.person is None:local_missing=True
            with ExitStack() as stack:
                def array(ref,shape):
                    local=stack.enter_context(artifacts.input(ref,suffix='.npy'));return stack.enter_context(read_array(local.path,shape,'float32',ctx.check_active,unit_interval=True))
                source=array(before.pixels,(before.canvas.height,before.canvas.width,3));target=array(after.pixels,(after.canvas.height,after.canvas.width,3));person=None if item.person is None else array(item.person.artifact,(after.canvas.height,after.canvas.width));ignore=None if item.ignore is None else array(item.ignore.artifact,(after.canvas.height,after.canvas.width))
                numeric=NumericAlignment(np.array(alignment.before_to_after).reshape(3,3),alignment.overlap,alignment.median_reprojection,alignment.inlier_count,alignment.confidence,alignment.crop_aware,tuple(alignment.warnings))
                pair,_=prepare_samples(str(item.pair_id),source,target,numeric,artifacts.scratch,person=person,ignore=ignore,seed=(params.seed+index)%(2**32),max_samples=params.samples_per_pair,tile=tile,check_active=ctx.check_active);samples.append(pair)
        if local_missing:raise ValueError('Local validation requires every person mask')
    except ValueError as exc:raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(1,4)
    candidate=_metrics(style,samples,True)
    # model_copy cannot change a discriminated subtype from active to Disabled.
    from photo_worker.contracts.style import Disabled
    identity_style=style.model_copy(update={'global_':identity_color(),'local':Disabled(enabled=False)})
    baseline=_metrics(identity_style,samples,False);improvement=(baseline.median_delta_e-candidate.median_delta_e)/max(baseline.median_delta_e,1e-12)
    sessions=len({p.session_id for p in params.holdout});reasons=[]
    if len(samples)<12:reasons.append('insufficient_pairs')
    if sessions<3:reasons.append('insufficient_sessions')
    if improvement<.1:reasons.append('median_improvement_below_10_percent')
    if candidate.p95_delta_e>baseline.p95_delta_e+.5:reasons.append('p95_regression')
    quality=Quality(status='measured' if not reasons else 'needs_review',reconstruction=style.quality.reconstruction,validation=candidate,beauty_validation=style.quality.beauty_validation,geometry_validation=style.quality.geometry_validation,transfer_confidence='experimental' if not reasons else 'unvalidated',warnings=list(dict.fromkeys([*style.quality.warnings,*reasons])))
    validated=style.model_copy(update={'quality':quality})
    ctx.progress(2,4);style_ref=artifacts.stage_contract(validated,'validated_style')
    # The report is provenance for the immutable validated artifact consumed by
    # style.package, not for the pre-validation aggregate candidate.
    report=StyleQualityReport(style=style_ref,config_hash=validated.config_hash,threshold_profile_id=params.threshold_profile_id,pair_count=len(samples),session_count=sessions,baseline=baseline,candidate=candidate,median_improvement_fraction=improvement,accepted_by_metrics=not reasons,resulting_quality=quality,reasons=reasons)
    report_ref=artifacts.stage_contract(report,'quality_report');ctx.progress(4,4)
    return OperationResult(outputs=[style_ref,report_ref],review_required=bool(reasons),bytes_written=len(canonical_bytes(validated))+len(canonical_bytes(report)))


def validate_operations(config:ArtifactConfig)->list[Operation]:
    def run(ctx):
        with TaskArtifacts.create(ctx,config,frozenset({'validated_style','quality_report'})) as artifacts:return execute(ctx,artifacts)
    return [Operation('style.validate','cpu',run)]

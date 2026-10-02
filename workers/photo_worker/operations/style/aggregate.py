from photo_worker.algorithms.style_aggregate import aggregate_color_fits
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.color_fit import ColorFit
from photo_worker.contracts.style import StyleV2, ColorSpace, Disabled, Extensions, Provenance, Quality,Beauty,Defects
from photo_worker.contracts.pair_effects import BeautyFit,DefectEvidence
from photo_worker.contracts.style_aggregate import StyleAggregateParams, StyleAggregateReport
from photo_worker.runtime.artifacts import TaskArtifacts, ArtifactConfig
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationResult, OperationFailure


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    if ctx.lease.execution_profile.get("backend") != "cpu":
        raise OperationFailure("CONFIG_INVALID")
    estimate = ctx.lease.execution_profile.get("resource_estimate")
    if not isinstance(estimate, dict) or type(estimate.get("ram_bytes")) is not int or estimate["ram_bytes"] < 536870912:
        raise OperationFailure("CONFIG_INVALID")
    ctx.progress(0, 4)
    try:
        params = artifacts.params(StyleAggregateParams)
        fits = [artifacts.contract(ref, ColorFit) for ref in params.color_fits]
        beauty_fits=[artifacts.contract(ref,BeautyFit) for ref in params.beauty_fits]
        defect_fits=[artifacts.contract(ref,DefectEvidence) for ref in params.defect_evidence]
        fit_by_pair={fit.params.pair_id:fit for fit in fits}
        for group in (beauty_fits,defect_fits):
            seen=set()
            for effect in group:
                pair_id=effect.params.pair_id
                color_fit=fit_by_pair.get(pair_id)
                if pair_id in seen or color_fit is None:
                    raise ValueError("EFFECT_PAIR_PROVENANCE_INVALID")
                seen.add(pair_id)
                if effect.params.before!=color_fit.params.before or effect.params.after!=color_fit.params.after:
                    raise ValueError("EFFECT_IMAGE_PROVENANCE_INVALID")
        global_color, local, evidence = aggregate_color_fits(fits)
        model_ids = {model.id for model in params.models}
        if local.enabled and "lraspp_mobilenet_v3_large" not in model_ids:
            raise ValueError("LOCAL_MODEL_LOCK_MISSING")
        ctx.progress(1, 4)
        warnings = list(evidence["warnings"])
        if len(fits) < 2:
            warnings.append("single_pair_unvalidated")
        accepted_beauty=[x.beauty for x in beauty_fits if x.beauty.enabled and x.confidence>=.02]
        beauty=Disabled(enabled=False)
        if accepted_beauty:
            import numpy as np
            values=np.asarray([[x.smoothing,x.tone_evenness,x.texture_keep] for x in accepted_beauty]);spread=float(np.max(np.ptp(values,axis=0))) if len(values)>1 else 0.
            if spread<=.35:
                median=np.median(values,axis=0);beauty=Beauty(enabled=True,algorithm='beautyfilter_v1',smoothing=float(median[0]),tone_evenness=float(median[1]),texture_keep=float(median[2]),skin_mask='face68_conservative_v1',protect_features=True)
            else:warnings.append('beauty_fit_inconsistent')
        accepted_defects=[x.defects for x in defect_fits if x.defects.enabled and x.confidence>=.75]
        defects=Disabled(enabled=False)
        if accepted_defects:
            import numpy as np
            defects=Defects(enabled=True,algorithm='defectrepair_telea_v1',policy='review',strength=float(np.median([x.strength for x in accepted_defects])),diameter_face_fraction=[.002,.012],preserve_permanent_marks=True)
        if (beauty.enabled or defects.enabled) and not {'sfd','fan2d'}<=model_ids:raise ValueError('EFFECT_MODEL_LOCK_MISSING')
        style = StyleV2(schema_version="2.0.0", renderer_version="photo_renderer_v2", style_id=params.style_id,
            mode="fixed", strength=1., color=ColorSpace(profile_id="work_rec2020_gamma24_v1", intent="relative", bpc=False),
            raw_recipe="raw_camera_wb_linear_srgb_ahd_v1", models=params.models, local=local,
            adaptation=None, beauty=beauty, defects=defects, geometry=Disabled(enabled=False),
            extensions=Extensions(), provenance=Provenance(pair_count=len(fits), session_count=len(set(params.session_ids)), seed=params.seed,
                manifest_sha256=params.pair_manifest_sha256, runtime_lock_id=params.runtime_lock_id),
            quality=Quality(status="needs_review", reconstruction=None, validation=None, transfer_confidence="unvalidated", warnings=warnings),
            **{"global": global_color})
        report = StyleAggregateReport(pair_ids=evidence["pair_ids"], pair_count=len(fits), session_count=len(set(params.session_ids)),
            accepted_local_fits=evidence["accepted_local_fits"], median_holdout_loss=evidence["median_holdout_loss"],
            median_delta_e=evidence["median_delta_e"], p95_delta_e=evidence["p95_delta_e"],
            transfer_confidence="unvalidated", warnings=warnings)
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc
    ctx.progress(2, 4)
    outputs = [artifacts.stage_contract(style, "style_candidate"), artifacts.stage_contract(report, "aggregate_report")]
    ctx.progress(4, 4)
    return OperationResult(outputs=outputs, review_required=True, bytes_written=len(canonical_bytes(style)) + len(canonical_bytes(report)))


def aggregate_operations(config: ArtifactConfig) -> list[Operation]:
    def aggregate(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"style_candidate", "aggregate_report"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("style.aggregate", "cpu", aggregate)]

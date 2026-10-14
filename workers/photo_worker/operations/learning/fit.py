"""V23 learning.fit adapter over an immutable TrainingManifest closure."""
from __future__ import annotations

from math import ceil
from uuid import uuid5

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.series import SeriesCorrections, SeriesParams
from photo_worker.operations.session.contracts import Calibration, DescriptorShard
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import (ExecutionContext, Operation,
                                           OperationFailure, OperationResult)

from .contracts import (CandidateProfile, FrozenRecipe, LearningFitParams,
                        TrainingManifest)
from .freeze import _session_order
from .descriptor import validate_training_descriptor
from .model import TrainExample, fit_delta_model
from .recipe import validate_frozen_render_case
from .detail_evidence import (FrozenPortraitDetailHoldoutEvidence,
                              PortraitDetailHoldoutExpected,
                              decode_frozen_detail_evidence,
                              validate_holdout_detail_binding)
from .series_evidence import (FrozenSeriesHoldoutEvidence,
                              decode_frozen_series_holdout_evidence)


def validate_fit_series_inputs(artifacts: TaskArtifacts, case,
                               event_id, training_run_id, tenant_id) -> None:
    """Bind declared series inputs to the frozen manifest row."""
    if case.series_evidence is None:
        return
    artifacts.require_declared_contract(case.series_evidence,
                                        'FrozenSeriesHoldoutEvidence')
    raw = artifacts.contract_bytes(case.series_evidence,
                                   FrozenSeriesHoldoutEvidence)
    parsed = FrozenSeriesHoldoutEvidence.model_validate_json(raw)
    evidence = decode_frozen_series_holdout_evidence(
        raw, case.series_evidence, parsed)
    if (case.series_evidence.artifact_id != uuid5(
            training_run_id, f'v23-series/{event_id}') or
            evidence.tenant_id != tenant_id or evidence.event_id != event_id or
            evidence.asset_version_id != case.binding.asset_version_id or
            evidence.source_image != case.working_image or
            any(ref is not None for ref in (
                case.defect_evidence, case.dust_evidence,
                case.detail_evidence))):
        raise ValueError('INVALID_HOLDOUT_SERIES_EVIDENCE')
    for ref, contract in (
            (evidence.source_image, 'WorkingImage'),
            (evidence.stage_image, 'WorkingImage'),
            (evidence.params, 'SeriesParams'),
            (evidence.series, 'SeriesCorrections')):
        artifacts.require_declared_contract(ref, contract)
    params = artifacts.contract(evidence.params, SeriesParams)
    series = artifacts.contract(evidence.series, SeriesCorrections)
    if (params.scene_id != series.scene_id or
            len(params.stats) != len(series.items) or
            len({ref.artifact_id for ref in params.stats}) != len(params.stats) or
            len({item.item_id for item in series.items}) != len(series.items) or
            evidence.item_id not in {item.item_id for item in series.items} or
            series.reference_item_id not in {
                item.item_id for item in series.items} or
            (params.reference_item_id is not None and
             params.reference_item_id != series.reference_item_id)):
        raise ValueError('INVALID_HOLDOUT_SERIES_MATERIAL')
    for stat in params.stats:
        artifacts.require_declared_contract(stat, 'SeriesStats')


def validate_fit_detail_inputs(artifacts: TaskArtifacts, case,
                               event_id, training_run_id, tenant_id) -> None:
    """Refuse missing or wrongly typed producer inputs before fitting."""
    if case.detail_evidence is None:
        return
    artifacts.require_declared_contract(case.detail_evidence,
        'FrozenPortraitDetailHoldoutEvidence')
    raw = artifacts.contract_bytes(case.detail_evidence,
                                   FrozenPortraitDetailHoldoutEvidence)
    parsed = FrozenPortraitDetailHoldoutEvidence.model_validate_json(raw)
    expected = PortraitDetailHoldoutExpected.model_validate(
        parsed.model_dump(mode='json', exclude={'schema_version'}))
    evidence = decode_frozen_detail_evidence(raw,
        case.detail_evidence, expected)
    validate_holdout_detail_binding(case, event_id, training_run_id,
        tenant_id, case.detail_evidence, evidence)
    for ref, contract in (
            (evidence.source, 'WorkingImage'),
            (evidence.image, 'WorkingImage'),
            (evidence.protection, 'MaskRef'),
            (evidence.params, 'PortraitParams'),
            (evidence.resolved, 'ResolvedStyle'),
            *((mask, 'NpyArray') for mask in evidence.mask_artifacts)):
        artifacts.require_declared_contract(ref, contract)


def fit_candidate(params: LearningFitParams, manifest: TrainingManifest,
                  baseline: CandidateProfile, calibration: Calibration, *,
                  check_active=lambda: None) -> CandidateProfile:
    """Verify the sealed partition again before fitting only its train rows."""
    check_active()
    manifest.identity_scope()
    if (params.manifest.sha256 == '0' * 64 or
            canonical_hash(params.policy) != manifest.policy_sha or
            params.base_profile != manifest.baseline or
            params.policy.mode != manifest.mode or
            params.policy.calibration != manifest.calibration or
            baseline.profile_id != manifest.profile_id or
            not calibration.qualified or
            calibration.model_lock_sha != manifest.model_lock_sha or
            (manifest.mode == 'person') != (manifest.person_id is not None)):
        raise ValueError('INVALID_TRAINING_MANIFEST')
    sessions = sorted({item.session_id for item in manifest.examples},
                      key=_session_order)
    if len(sessions) < 6:
        raise ValueError('INSUFFICIENT_DATA')
    holdout_count = max(2, ceil(0.2 * len(sessions)))
    holdout = set(sessions[:holdout_count])
    event_ids = set()
    lineages = set()
    source_hashes = set()
    train = []
    holdout_rows = 0
    for item in manifest.examples:
        check_active()
        if (item.event_id.int == 0 or item.session_id.int == 0 or
                item.subject_group_id.int == 0 or item.event_id in event_ids or
                item.render_case.source_lineage_sha in lineages or
                item.render_case.working_image.sha256 in source_hashes or
                (item.split == 'holdout') != (item.session_id in holdout) or
                item.render_case.binding.source != item.render_case.working_image or
                item.render_case.approval_id.int == 0 or
                (manifest.mode == 'person' and
                 item.subject_group_id != manifest.person_id)):
            raise ValueError('INVALID_TRAINING_MANIFEST')
        event_ids.add(item.event_id)
        lineages.add(item.render_case.source_lineage_sha)
        source_hashes.add(item.render_case.working_image.sha256)
        if item.split == 'holdout':
            holdout_rows += 1
            continue
        target = item.target
        train.append(TrainExample(item.event_id, item.session_id,
                                  tuple(item.features),
                                  (target.ev, target.log_r, target.log_g,
                                   target.log_b, target.saturation_delta,
                                   target.retouch_delta)))
    if holdout_rows < 10 or len(train) < 20:
        raise ValueError('INSUFFICIENT_DATA')
    model = fit_delta_model(train, tuple(calibration.loss_weights),
                            tuple(calibration.normalization_scales),
                            check_active=check_active)
    return CandidateProfile(schema_version=1, profile_id=manifest.profile_id,
                            training_sha=params.manifest.sha256,
                            feature_order='scene12_v1', means=list(model.means),
                            scales=list(model.scales),
                            coefficients=[list(row) for row in model.coefficients],
                            intercept=list(model.intercept),
                            ridge_lambda=model.ridge_lambda)


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    if (ctx.lease.execution_profile.get('renderer_version') != 'v2.2' or
            ctx.lease.execution_profile.get('backend') != 'cpu'):
        raise OperationFailure('CONFIG_INVALID')
    try:
        ctx.check_active()
        params = artifacts.params(LearningFitParams)
        ctx.check_active()
        manifest = artifacts.contract(params.manifest, TrainingManifest)
        ctx.check_active()
        baseline = artifacts.contract(params.base_profile, CandidateProfile)
        ctx.check_active()
        calibration = artifacts.contract(params.policy.calibration, Calibration)
        ctx.check_active()
        for item in manifest.examples:
            ctx.check_active()
            case = item.render_case
            descriptor = artifacts.contract(case.descriptor_shard, DescriptorShard)
            ctx.check_active()
            validate_training_descriptor(item, descriptor)
            base_recipe = artifacts.contract(case.base_recipe, FrozenRecipe)
            ctx.check_active()
            approved_recipe = artifacts.contract(case.approved_recipe, FrozenRecipe)
            ctx.check_active()
            validate_frozen_render_case(case, base_recipe, approved_recipe, manifest)
            validate_fit_detail_inputs(artifacts, case, item.event_id,
                ctx.lease.command.run_id, ctx.lease.command.tenant_id)
            validate_fit_series_inputs(artifacts, case, item.event_id,
                ctx.lease.command.run_id, ctx.lease.command.tenant_id)
        result = fit_candidate(params, manifest, baseline, calibration,
                               check_active=ctx.check_active)
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ctx.check_active()
    ref = artifacts.stage_contract(result, 'document')
    ctx.check_active()
    return OperationResult(outputs=[ref], bytes_written=len(canonical_bytes(result)))


def learning_fit_operations(config: ArtifactConfig) -> list[Operation]:
    def run(ctx: ExecutionContext) -> OperationResult:
        with TaskArtifacts.create(ctx, config, frozenset({'document'})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation('learning.fit', 'cpu', run)]

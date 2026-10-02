"""V23 learning.fit adapter over an immutable TrainingManifest closure."""
from __future__ import annotations

from math import ceil

from photo_worker.contracts.base import canonical_bytes, canonical_hash
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


def fit_candidate(params: LearningFitParams, manifest: TrainingManifest,
                  baseline: CandidateProfile, calibration: Calibration) -> CandidateProfile:
    """Verify the sealed partition again before fitting only its train rows."""
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
    train = []
    holdout_rows = 0
    for item in manifest.examples:
        if (item.event_id.int == 0 or item.session_id.int == 0 or
                item.subject_group_id.int == 0 or item.event_id in event_ids or
                item.render_case.source_lineage_sha in lineages or
                (item.split == 'holdout') != (item.session_id in holdout) or
                item.render_case.binding.source != item.render_case.working_image or
                item.render_case.approval_id.int == 0 or
                (manifest.mode == 'person' and
                 item.subject_group_id != manifest.person_id)):
            raise ValueError('INVALID_TRAINING_MANIFEST')
        event_ids.add(item.event_id)
        lineages.add(item.render_case.source_lineage_sha)
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
                            tuple(calibration.normalization_scales))
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
        params = artifacts.params(LearningFitParams)
        manifest = artifacts.contract(params.manifest, TrainingManifest)
        baseline = artifacts.contract(params.base_profile, CandidateProfile)
        calibration = artifacts.contract(params.policy.calibration, Calibration)
        for item in manifest.examples:
            ctx.check_active()
            case = item.render_case
            validate_training_descriptor(item, artifacts.contract(
                case.descriptor_shard, DescriptorShard))
            validate_frozen_render_case(
                case, artifacts.contract(case.base_recipe, FrozenRecipe),
                artifacts.contract(case.approved_recipe, FrozenRecipe), manifest)
        result = fit_candidate(params, manifest, baseline, calibration)
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ref = artifacts.stage_contract(result, 'document')
    return OperationResult(outputs=[ref], bytes_written=len(canonical_bytes(result)))


def learning_fit_operations(config: ArtifactConfig) -> list[Operation]:
    def run(ctx: ExecutionContext) -> OperationResult:
        with TaskArtifacts.create(ctx, config, frozenset({'document'})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation('learning.fit', 'cpu', run)]

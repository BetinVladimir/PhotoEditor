"""V22 session.plan execution over an authenticated, frozen input closure."""
from __future__ import annotations

from hashlib import sha256

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult
from .contracts import (Calibration, CorrectionItem, CorrectionPlan, CorrectionVector,
                        DescriptorShard, ScenePlan, ScenePlanGroup, SessionPlanContext,
                        SessionPlanParams)
from .plan import (Frame, SceneCalibration, ScenePolicy, group_scenes,
                   solve_consistency, validate_scene_groups)


def _plan(artifacts: TaskArtifacts, backend: str) -> tuple[ScenePlan, int]:
    if backend != 'cpu':
        raise OperationFailure('CONFIG_INVALID')
    params = artifacts.params(SessionPlanParams)
    context = artifacts.contract(params.context, SessionPlanContext)
    calibration = artifacts.contract(params.policy.calibration, Calibration)
    if not calibration.qualified:
        raise OperationFailure('MODEL_UNQUALIFIED')
    if len(set((r.artifact_id, r.sha256) for r in params.shards)) != len(params.shards):
        raise ValueError('DUPLICATE_DESCRIPTOR_SHARD')
    offsets = {entry.camera_hash: entry.seconds for entry in params.policy.camera_offsets}
    if len(offsets) != len(params.policy.camera_offsets):
        raise ValueError('DUPLICATE_CAMERA_OFFSET')
    descriptors = []
    for ref in params.shards:
        shard = artifacts.contract(ref, DescriptorShard)
        if shard.snapshot_digest != context.snapshot_digest:
            raise ValueError('SNAPSHOT_DIGEST_MISMATCH')
        descriptors.extend(shard.frames)
    if not 1 <= len(descriptors) <= 5000:
        raise ValueError('INVALID_DESCRIPTOR_COUNT')
    by_ordinal = {item.ordinal: item for item in descriptors}
    by_asset = {item.binding.asset_version_id: item.ordinal for item in descriptors}
    if len(by_ordinal) != len(descriptors) or len(by_asset) != len(descriptors):
        raise ValueError('DUPLICATE_DESCRIPTOR_BINDING')
    if set(by_ordinal) != set(range(len(descriptors))):
        raise ValueError('NONCONTIGUOUS_DESCRIPTOR_ORDINAL')
    descriptors = [by_ordinal[ordinal] for ordinal in range(len(descriptors))]
    locks = frozenset(context.manual_locked_ordinals)
    if len(locks) != len(context.manual_locked_ordinals) or not locks <= by_ordinal.keys():
        raise ValueError('INVALID_MANUAL_LOCKS')
    manual = {}
    manual_groups = {}
    for item in context.manual_references:
        expected = frozenset(item.previous_members)
        if (len(expected) != len(item.previous_members) or item.asset_version_id not in expected
                or not expected <= by_asset.keys() or item.asset_version_id in manual):
            raise ValueError('INVALID_MANUAL_REFERENCE')
        if expected in manual_groups:
            raise ValueError('CONFLICTING_MANUAL_REFERENCE')
        manual[item.asset_version_id] = expected
        manual_groups[expected] = item.asset_version_id
    frames = [Frame(item.ordinal, item.captured_at, item.camera_hash,
                    tuple(item.scene_embedding), item.luma_quantiles[2],
                    None if item.neutral_rgb is None else tuple(item.neutral_rgb),
                    item.sharpness, item.clipping_fraction)
              for item in descriptors]
    policy = ScenePolicy(params.policy.time_gap_seconds, params.policy.neighbor_count, offsets)
    calibrated = SceneCalibration(calibration.scene_cosine_min, calibration.scene_log_luma_max,
                                  calibration.cluster_diameter_max, calibration.sharpness_scale,
                                  calibration.clipping_penalty)
    policy_sha = sha256(canonical_bytes(params.policy)).hexdigest()
    groups = group_scenes(frames, context.snapshot_id, policy_sha, policy, calibrated)
    validate_scene_groups(groups, frames, context.snapshot_id, policy_sha)
    for asset_id, expected in manual.items():
        ordinal = by_asset[asset_id]
        actual = next((g for g in groups if ordinal in g.ordinals), None)
        if actual is None or frozenset(by_ordinal[o].binding.asset_version_id for o in actual.ordinals) != expected:
            raise ValueError('REFERENCE_GROUP_CHANGED')
    staged = []
    total_bytes = 0
    for group in groups:
        group_frames = [frames[o] for o in group.ordinals]
        member_ids = frozenset(by_ordinal[o].binding.asset_version_id for o in group.ordinals)
        manual_asset = manual_groups.get(member_ids)
        selected = by_asset[manual_asset] if manual_asset is not None else None
        if selected is not None and selected != group.reference_ordinal:
            from .plan import choose_reference
            reference_ordinal = choose_reference(group_frames, calibrated, selected)
        else:
            reference_ordinal = group.reference_ordinal
        corrections = solve_consistency(group_frames, reference_ordinal, locks)
        document = CorrectionPlan(schema_version=1, snapshot_digest=context.snapshot_digest,
                                  items=[CorrectionItem(binding=by_ordinal[c.ordinal].binding,
                                                        ordinal=c.ordinal,
                                                        delta=CorrectionVector(ev=c.ev, log_r=c.log_r,
                                                                               log_g=c.log_g, log_b=c.log_b,
                                                                               saturation_delta=c.saturation_delta,
                                                                               retouch_delta=c.retouch_delta),
                                                        disposition=c.disposition, reason=c.reason)
                                         for c in corrections])
        total_bytes += len(canonical_bytes(document))
        ref = artifacts.stage_contract(document, 'correction_plan')
        staged.append(ScenePlanGroup(id=group.id, ordinals=list(group.ordinals),
                                     reference_ordinal=reference_ordinal,
                                     confidence=group.confidence, corrections=ref))
    result = ScenePlan(schema_version=1, snapshot_id=context.snapshot_id,
                       snapshot_digest=context.snapshot_digest, policy_sha=policy_sha,
                       groups=staged)
    return result, total_bytes + len(canonical_bytes(result))


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    if ctx.lease.execution_profile.get('renderer_version') != 'v2.2':
        raise OperationFailure('CONFIG_INVALID')
    try:
        result, total_bytes = _plan(artifacts, ctx.lease.execution_profile.get('backend'))
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    root = artifacts.stage_contract(result, 'document')
    # A scene plan is a decision proposal. The parent workflow must remain
    # waiting_review until ApplyAnalysis records the final group selection.
    return OperationResult(outputs=[root], review_required=True, bytes_written=total_bytes)


def session_plan_operations(config: ArtifactConfig) -> list[Operation]:
    def run(ctx: ExecutionContext) -> OperationResult:
        with TaskArtifacts.create(ctx, config, frozenset({'document', 'correction_plan'})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation('session.plan', 'cpu', run)]

from datetime import datetime, timezone
from hashlib import sha256
from types import SimpleNamespace
from uuid import UUID

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.operations.session.adapter import _plan, execute
from photo_worker.operations.session.contracts import (Calibration, DescriptorShard,
    SessionPlanContext, SessionPlanParams)


def ref(n):
    return ArtifactRef(artifact_id=UUID(int=n), sha256=f'{n:064x}')


def frame(ordinal, asset):
    return dict(schema_version=1, binding=dict(asset_version_id=UUID(int=asset),
                source=ref(asset + 100), canvas_hash='a' * 64),
                ordinal=ordinal, captured_at=datetime(2026, 1, 1, tzinfo=timezone.utc),
                camera_hash=None, scene_embedding=[1.0] + [0.0] * 383,
                luma_quantiles=[.1, .2, .5 + ordinal * .1, .7, .9],
                neutral_rgb=[.5, .5, .5], sharpness=100 + ordinal,
                clipping_fraction=0, person_fraction=0,
                faces=ref(asset + 200))


def calibration():
    return Calibration.model_validate(dict(schema_version=1, model_lock_sha='a'*64,
        dataset_sha='b'*64, qualified=True, scene_cosine_min=.9,
        scene_log_luma_max=1, cluster_diameter_max=.1,
        sharpness_scale=100, clipping_penalty=1, match_inlier_min=.5,
        match_error_px_max=2, identity_similarity_min=.5, landmark_drift_max=.5,
        normalization_scales=[1]*6, loss_weights=[.4,.1,.1,.1,.2,.1], focus_warning=.5,
        focus_critical=.2, eye_open_min=.5, glare_warning_fraction=.5,
        clipping_warning_fraction=.5, clipping_critical_fraction=.9,
        min_face_edge_px=32, depth_sigma=.1, registration_error_px=1,
        motion_threshold=.1))


class Artifacts:
    def __init__(self, *, manual=False, wrong_digest=False):
        self.params_value = SessionPlanParams.model_validate(dict(schema_version=1,
            shards=[ref(1)], context=ref(2), policy=dict(schema_version=1,
            time_gap_seconds=60, neighbor_count=2, protect_manual_picks=True,
            camera_offsets=[], calibration=ref(3))))
        self.context = SessionPlanContext.model_validate(dict(schema_version=1,
            snapshot_id=UUID(int=9), snapshot_digest='c'*64,
            manual_references=[dict(asset_version_id=UUID(int=10),
                previous_members=[UUID(int=10), UUID(int=11)])] if manual else [],
            manual_locked_ordinals=[0] if manual else []))
        self.shard = DescriptorShard.model_validate(dict(schema_version=1,
            snapshot_digest='d'*64 if wrong_digest else 'c'*64,
            frames=[frame(1, 11), frame(0, 10)]))
        self.staged = []

    def params(self, model):
        return self.params_value

    def contract(self, item, model):
        return {SessionPlanContext: self.context, Calibration: calibration(),
                DescriptorShard: self.shard}[model]

    def stage_contract(self, value, role):
        self.staged.append((role, value))
        return ref(len(self.staged) + 50)


def test_session_adapter_stages_child_before_root_with_manual_reference_and_lock():
    artifacts = Artifacts(manual=True)
    plan, size = _plan(artifacts, 'cpu')
    assert size >= len(canonical_bytes(plan))
    assert [role for role, _ in artifacts.staged] == ['correction_plan']
    assert len(plan.groups) == 1
    assert plan.groups[0].reference_ordinal == 0
    assert plan.groups[0].corrections == ref(51)
    assert [item.disposition for item in artifacts.staged[0][1].items] == ['manual_locked', 'apply']
    assert plan.policy_sha == sha256(canonical_bytes(artifacts.params_value.policy)).hexdigest()


def test_session_adapter_rejects_rebound_snapshot_before_stage():
    artifacts = Artifacts(wrong_digest=True)
    with pytest.raises(ValueError, match='SNAPSHOT_DIGEST_MISMATCH'):
        _plan(artifacts, 'cpu')
    assert artifacts.staged == []


def test_manual_lock_is_applied_only_to_its_scene_group():
    artifacts = Artifacts()
    artifacts.context = artifacts.context.model_copy(update={'manual_locked_ordinals': [0]})
    later = frame(1, 11)
    later['captured_at'] = datetime(2026, 1, 2, tzinfo=timezone.utc)
    artifacts.shard = DescriptorShard.model_validate(dict(schema_version=1,
        snapshot_digest='c'*64, frames=[later, frame(0, 10)]))
    plan, _ = _plan(artifacts, 'cpu')
    assert len(plan.groups) == 2
    assert [role for role, _ in artifacts.staged] == ['correction_plan', 'correction_plan']
    assert artifacts.staged[0][1].items[0].disposition == 'manual_locked'
    assert artifacts.staged[1][1].items[0].disposition == 'no_change'


def test_session_plan_requires_review_before_parent_finalization():
    artifacts = Artifacts()
    ctx = SimpleNamespace(lease=SimpleNamespace(execution_profile={
        'renderer_version': 'v2.2', 'backend': 'cpu'}))
    result = execute(ctx, artifacts)
    assert result.review_required is True
    assert [role for role, _ in artifacts.staged] == ['correction_plan', 'document']
    assert result.outputs == [ref(52)]

"""Strict worker-side contracts for the V22 session planner."""
from __future__ import annotations

import math
from datetime import datetime
from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, Unit

Ordinal = Annotated[int, Field(ge=0, le=4999)]


class VersionBinding(Contract):
    asset_version_id: UUID
    source: ArtifactRef
    canvas_hash: SHA256


class FrameDescriptor(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    ordinal: Ordinal
    captured_at: datetime | None
    camera_hash: SHA256 | None
    scene_embedding: Annotated[list[Annotated[float, Field(ge=-1, le=1)]], Field(min_length=384, max_length=384)]
    luma_quantiles: Annotated[list[Unit], Field(min_length=5, max_length=5)]
    neutral_rgb: Annotated[list[Unit], Field(min_length=3, max_length=3)] | None
    sharpness: Annotated[float, Field(ge=0, le=1_000_000)]
    clipping_fraction: Unit
    person_fraction: Unit
    faces: ArtifactRef


class DescriptorShard(Contract):
    schema_version: Literal[1]
    snapshot_digest: SHA256
    frames: Annotated[list[FrameDescriptor], Field(min_length=1, max_length=128)]


class SnapshotItem(Contract):
    ordinal: Ordinal
    binding: VersionBinding
    working_image: ArtifactRef
    captured_at: datetime | None
    camera_hash: SHA256 | None
    recipe: ArtifactRef


class InputSnapshot(Contract):
    schema_version: Literal[1]
    snapshot_id: UUID
    snapshot_digest: SHA256
    items: Annotated[list[SnapshotItem], Field(min_length=1, max_length=5000)]


class SessionDescribeParams(Contract):
    schema_version: Literal[1]
    snapshot: ArtifactRef
    ordinals: Annotated[list[Ordinal], Field(min_length=1, max_length=128)]
    model_lock_sha: SHA256
    calibration: ArtifactRef
    consents: ArtifactRef


class ConsentGrant(Contract):
    consent_id: UUID
    epoch: Annotated[str, Field(pattern=r'^[1-9][0-9]{0,18}$')]
    person_id: UUID | None
    purpose: Literal['identity_matching', 'learning_photographer', 'learning_person']


class ConfirmedFace(Contract):
    asset_version_id: UUID
    face_id: UUID
    person_id: UUID
    consent_id: UUID
    epoch: Annotated[str, Field(pattern=r'^[1-9][0-9]{0,18}$')]


class ConsentSnapshot(Contract):
    schema_version: Literal[1]
    grants: Annotated[list[ConsentGrant], Field(max_length=5000)]
    confirmed_faces: Annotated[list[ConfirmedFace], Field(max_length=12800)]


class FaceRect(Contract):
    x0: Unit
    y0: Unit
    x1: Unit
    y1: Unit


    @model_validator(mode='after')
    def ordered_edges(self):
        if self.x0 >= self.x1 or self.y0 >= self.y1:
            raise ValueError('INVALID_FACE_RECT')
        return self


class FacePoint(Contract):
    x: Unit
    y: Unit


class FaceObservation(Contract):
    observation_id: UUID
    box: FaceRect
    landmarks: Annotated[list[FacePoint], Field(min_length=68, max_length=68)]
    embedding: Annotated[list[Annotated[float, Field(ge=-1, le=1)]], Field(min_length=512, max_length=512)] | None
    quality: Unit
    person_id: UUID | None
    face_id: UUID


    @model_validator(mode='after')
    def observation_identity(self):
        if (self.observation_id.int == 0 or self.face_id.int == 0
                or (self.person_id is None) != (self.embedding is None)
                or self.person_id is not None and self.person_id.int == 0):
            raise ValueError('INVALID_FACE_OBSERVATION_IDENTITY')
        return self


class FaceObservations(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    faces: Annotated[list[FaceObservation], Field(max_length=100)]


    @model_validator(mode='after')
    def scoped_unique_faces(self):
        if (self.binding.asset_version_id.int == 0
                or self.binding.source.artifact_id.int == 0
                or len({face.observation_id for face in self.faces}) != len(self.faces)
                or len({face.face_id for face in self.faces}) != len(self.faces)):
            raise ValueError('INVALID_FACE_OBSERVATIONS_SCOPE')
        return self


class CameraOffset(Contract):
    camera_hash: SHA256
    seconds: Annotated[int, Field(ge=-86400, le=86400)]


class SessionPolicy(Contract):
    schema_version: Literal[1]
    time_gap_seconds: Annotated[int, Field(ge=30, le=7200)]
    neighbor_count: Annotated[int, Field(ge=1, le=64)]
    protect_manual_picks: Literal[True]
    camera_offsets: Annotated[list[CameraOffset], Field(max_length=32)]
    calibration: ArtifactRef


class SessionPlanParams(Contract):
    schema_version: Literal[1]
    shards: Annotated[list[ArtifactRef], Field(min_length=1, max_length=40)]
    policy: SessionPolicy
    context: ArtifactRef


class ManualReference(Contract):
    asset_version_id: UUID
    previous_members: Annotated[list[UUID], Field(min_length=1, max_length=5000)]


class SessionPlanContext(Contract):
    schema_version: Literal[1]
    snapshot_id: UUID
    snapshot_digest: SHA256
    manual_references: Annotated[list[ManualReference], Field(max_length=5000)]
    manual_locked_ordinals: Annotated[list[Ordinal], Field(max_length=5000)]


class Calibration(Contract):
    schema_version: Literal[1]
    model_lock_sha: SHA256
    dataset_sha: SHA256
    qualified: bool
    scene_cosine_min: Annotated[float, Field(ge=-1, le=1)]
    scene_log_luma_max: Annotated[float, Field(ge=0, le=4)]
    cluster_diameter_max: Annotated[float, Field(ge=0, le=2)]
    sharpness_scale: Annotated[float, Field(ge=1e-6, le=1e6)]
    clipping_penalty: Annotated[float, Field(ge=0, le=10)]
    match_inlier_min: Unit
    match_error_px_max: Annotated[float, Field(ge=0, le=100)]
    identity_similarity_min: Annotated[float, Field(ge=-1, le=1)]
    landmark_drift_max: Unit
    normalization_scales: Annotated[list[Annotated[float, Field(ge=1e-6, le=1e6)]], Field(min_length=6, max_length=6)]
    loss_weights: Annotated[list[Unit], Field(min_length=6, max_length=6)]
    focus_warning: Unit
    focus_critical: Unit
    eye_open_min: Unit
    glare_warning_fraction: Unit
    clipping_warning_fraction: Unit
    clipping_critical_fraction: Unit
    min_face_edge_px: Annotated[int, Field(ge=32, le=512)]
    depth_sigma: Annotated[float, Field(ge=.01, le=1)]
    registration_error_px: Annotated[float, Field(ge=.01, le=10)]
    motion_threshold: Annotated[float, Field(ge=.001, le=1)]

    @model_validator(mode='after')
    def normalized_loss_weights(self):
        if abs(math.fsum(self.loss_weights)-1.0)>1e-9:
            raise ValueError('Calibration loss_weights must sum to one')
        return self


class CorrectionVector(Contract):
    ev: Annotated[float, Field(ge=-1, le=1)]
    log_r: Annotated[float, Field(ge=-.223144, le=.223144)]
    log_g: Annotated[float, Field(ge=-.223144, le=.223144)]
    log_b: Annotated[float, Field(ge=-.223144, le=.223144)]
    saturation_delta: Annotated[float, Field(ge=-.15, le=.15)]
    retouch_delta: Annotated[float, Field(ge=-.15, le=.15)]


class CorrectionItem(Contract):
    binding: VersionBinding
    ordinal: Ordinal
    delta: CorrectionVector
    disposition: Literal['apply', 'no_change', 'manual_locked']
    reason: Annotated[str, Field(min_length=1, max_length=80)] | None


class CorrectionPlan(Contract):
    schema_version: Literal[1]
    snapshot_digest: SHA256
    items: Annotated[list[CorrectionItem], Field(min_length=1, max_length=5000)]


class ScenePlanGroup(Contract):
    id: UUID
    ordinals: Annotated[list[Ordinal], Field(min_length=1, max_length=5000)]
    reference_ordinal: Ordinal
    confidence: Unit
    corrections: ArtifactRef


class ScenePlan(Contract):
    schema_version: Literal[1]
    snapshot_id: UUID
    snapshot_digest: SHA256
    policy_sha: SHA256
    groups: Annotated[list[ScenePlanGroup], Field(min_length=1, max_length=5000)]

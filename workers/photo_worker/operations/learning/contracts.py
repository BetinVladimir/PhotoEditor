"""Strict V23 learning.fit transport contracts."""
from __future__ import annotations

from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256
from photo_worker.operations.session.contracts import CorrectionVector, VersionBinding


class LearningConsent(Contract):
    consent_id: UUID
    epoch: Annotated[str, Field(pattern=r'^[1-9][0-9]{0,18}$')]


class HoldoutRegionMask(Contract):
    region_id: Annotated[str, Field(min_length=1, max_length=120)]
    mask: ArtifactRef


class RenderCase(Contract):
    binding: VersionBinding
    working_image: ArtifactRef
    base_recipe: ArtifactRef
    approved_recipe: ArtifactRef
    base_document: ArtifactRef
    approved_document: ArtifactRef
    descriptor_shard: ArtifactRef
    descriptor_analysis_id: UUID
    descriptor_ordinal: Annotated[int, Field(ge=0, le=4999)]
    protected: ArtifactRef
    faces: ArtifactRef
    effective_mask: ArtifactRef | None = Field(
        default=None, exclude_if=lambda value: value is None)
    consent_snapshot: ArtifactRef | None = Field(
        default=None, exclude_if=lambda value: value is None)
    region_masks: Annotated[list[HoldoutRegionMask], Field(max_length=128)] = Field(
        default_factory=list, exclude_if=lambda value: len(value) == 0)
    approval_id: UUID
    source_lineage_sha: SHA256

    @model_validator(mode='after')
    def identity_evidence_pair(self):
        if (self.effective_mask is None) != (self.consent_snapshot is None):
            raise ValueError('INCOMPLETE_HOLDOUT_IDENTITY_EVIDENCE')
        if len({item.region_id for item in self.region_masks}) != len(self.region_masks):
            raise ValueError('DUPLICATE_HOLDOUT_REGION_MASK')
        return self


class FrozenRecipe(Contract):
    schema_version: Literal[1]
    source: VersionBinding
    base_style: ArtifactRef
    effective_delta: CorrectionVector
    renderer_version: Literal['v2.2']
    icc_lock_sha: SHA256
    model_lock_sha: SHA256


class TrainingExample(Contract):
    event_id: UUID
    session_id: UUID
    subject_group_id: UUID
    split: Literal['train', 'holdout']
    features: Annotated[list[Annotated[float, Field(ge=-100, le=100)]],
                        Field(min_length=12, max_length=12)]
    target: CorrectionVector
    render_case: RenderCase


class TrainingManifest(Contract):
    schema_version: Literal[1]
    profile_id: UUID
    mode: Literal['photographer', 'person']
    person_id: UUID | None
    consents: Annotated[list[LearningConsent], Field(min_length=1, max_length=5000)]
    examples: Annotated[list[TrainingExample], Field(min_length=40, max_length=5000)]
    policy_sha: SHA256
    baseline: ArtifactRef
    renderer_version: Literal['v2.2']
    icc_lock_sha: SHA256
    model_lock_sha: SHA256
    calibration: ArtifactRef


class LearningPolicy(Contract):
    schema_version: Literal[1]
    mode: Literal['photographer', 'person']
    min_examples: Literal[40]
    min_sessions: Literal[6]
    holdout_fraction: Literal[0.2]
    seed: Literal[2026]
    calibration: ArtifactRef


class LearningFitParams(Contract):
    schema_version: Literal[1]
    manifest: ArtifactRef
    base_profile: ArtifactRef
    policy: LearningPolicy


class CandidateProfile(Contract):
    schema_version: Literal[1]
    profile_id: UUID
    training_sha: SHA256
    feature_order: Literal['scene12_v1']
    means: Annotated[list[Annotated[float, Field(ge=-100, le=100)]],
                     Field(min_length=12, max_length=12)]
    scales: Annotated[list[Annotated[float, Field(ge=1e-8, le=1e8)]],
                      Field(min_length=12, max_length=12)]
    coefficients: Annotated[list[Annotated[list[Annotated[float,
                        Field(ge=-1e6, le=1e6)]], Field(min_length=6, max_length=6)]],
                            Field(min_length=12, max_length=12)]
    intercept: Annotated[list[Annotated[float, Field(ge=-1e6, le=1e6)]],
                         Field(min_length=6, max_length=6)]
    ridge_lambda: Literal[0.01, 0.1, 1.0, 10.0]


class LearningEvaluateParams(Contract):
    schema_version: Literal[1]
    candidate: ArtifactRef
    manifest: ArtifactRef
    baseline: ArtifactRef
    calibration: ArtifactRef
    model_lock_sha: SHA256
    icc_lock_sha: SHA256
    renderer_version: Literal['v2.2']


class EvaluationCase(Contract):
    event_id: UUID
    color_delta: Annotated[float, Field(ge=-100, le=100)]
    clipping_delta: Annotated[float, Field(ge=-1, le=1)]
    identity_status: Literal['pass', 'fail', 'not_measurable', 'no_person']
    protected_max_error: Annotated[float, Field(ge=0, le=1_000_000)]


class Evaluation(Contract):
    schema_version: Literal[1]
    candidate_sha: SHA256
    manifest_sha: SHA256
    holdout_sessions: Annotated[int, Field(ge=0, le=5000)]
    baseline_loss: Annotated[float, Field(ge=0, le=1e9)]
    candidate_loss: Annotated[float, Field(ge=0, le=1e9)]
    color_p95_delta: Annotated[float, Field(ge=-100, le=100)]
    clipping_delta_fraction: Annotated[float, Field(ge=-1, le=1)]
    safety_regression: bool
    eligible: bool
    reasons: Annotated[list[Annotated[str, Field(min_length=1, max_length=80)]],
                       Field(max_length=32)]
    metric_eligible: bool
    safety_status: Literal['pass', 'fail', 'not_measurable']
    cases: Annotated[list[EvaluationCase], Field(min_length=10, max_length=5000)]

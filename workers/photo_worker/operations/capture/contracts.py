"""V24 frozen capture inputs and QA output contracts."""

from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, Unit
from photo_worker.operations.session.contracts import FaceRect, VersionBinding

CheckCode = Literal['focus', 'eyes', 'clipping', 'glare', 'duplicate', 'shot_list']


class CapturePolicy(Contract):
    schema_version: Literal[1]
    checks: Annotated[list[CheckCode], Field(min_length=1, max_length=6)]
    max_queue_age_seconds: Literal[30]
    calibration: ArtifactRef

    @model_validator(mode='after')
    def unique_checks(self):
        if len(set(self.checks)) != len(self.checks):
            raise ValueError('DUPLICATE_CAPTURE_CHECK')
        return self


class CaptureAssessParams(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    proxy: ArtifactRef
    faces: ArtifactRef
    policy: CapturePolicy
    model_lock_sha: SHA256
    context: ArtifactRef


class Coverage(Contract):
    requirement_id: UUID
    confirmed_frames: Annotated[int, Field(ge=0, le=5000)]


class CaptureContext(Contract):
    schema_version: Literal[1]
    session_id: UUID
    confirmed_person_ids: Annotated[list[UUID], Field(max_length=100)]
    shot_list: ArtifactRef
    recent_content_shas: Annotated[list[SHA256], Field(max_length=5000)]
    consents: ArtifactRef
    coverage: Annotated[list[Coverage], Field(max_length=200)]

    @model_validator(mode='after')
    def unique_context(self):
        if len(set(self.confirmed_person_ids)) != len(self.confirmed_person_ids):
            raise ValueError('DUPLICATE_CONFIRMED_PERSON')
        if len({item.requirement_id for item in self.coverage}) != len(self.coverage):
            raise ValueError('DUPLICATE_COVERAGE_REQUIREMENT')
        return self


class ShotRequirement(Contract):
    id: UUID
    label: Annotated[str, Field(min_length=1, max_length=200)]
    person_ids: Annotated[list[UUID], Field(max_length=100)]
    min_confirmed_frames: Annotated[int, Field(ge=1, le=100)]
    min_quality: Unit

    @model_validator(mode='after')
    def unique_persons(self):
        if len(set(self.person_ids)) != len(self.person_ids):
            raise ValueError('DUPLICATE_SHOT_PERSON')
        return self


class ShotList(Contract):
    schema_version: Literal[1]
    requirements: Annotated[list[ShotRequirement], Field(max_length=200)]

    @model_validator(mode='after')
    def unique_requirements(self):
        if len({item.id for item in self.requirements}) != len(self.requirements):
            raise ValueError('DUPLICATE_SHOT_REQUIREMENT')
        return self


class QACheck(Contract):
    code: Annotated[str, Field(min_length=1, max_length=80)]
    status: Literal['pass', 'warning', 'critical', 'unavailable']
    face_id: UUID | None
    score: Unit | None
    threshold: Annotated[float, Field(ge=-1e6, le=1e6)] | None
    region: FaceRect | None
    reason: Annotated[str, Field(min_length=1, max_length=80)] | None

    @model_validator(mode='after')
    def unavailable_is_not_clean(self):
        if self.status == 'unavailable' and (self.score is not None or self.reason is None):
            raise ValueError('INVALID_UNAVAILABLE_CHECK')
        return self


class QAReport(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    checks: Annotated[list[QACheck], Field(min_length=1, max_length=600)]


class CapturePrepareParams(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    calibration: ArtifactRef
    model_lock_sha: SHA256


class CapturePrepared(Contract):
    schema_version: Literal[1]
    binding: VersionBinding
    proxy: ArtifactRef
    faces: ArtifactRef

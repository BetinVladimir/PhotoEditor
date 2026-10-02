"""Strict transport contracts for V23 identity.compare."""
from __future__ import annotations

from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256
from photo_worker.operations.session.contracts import VersionBinding


class IdentityCompareParams(Contract):
    schema_version: Literal[1]
    before: VersionBinding
    after: VersionBinding
    effective_mask: ArtifactRef
    protected: ArtifactRef
    model_lock_sha: SHA256
    calibration: ArtifactRef
    consents: ArtifactRef


class IdentityFaceMetric(Contract):
    face_id: UUID
    similarity: Annotated[float, Field(ge=-1, le=1)] | None
    landmark_drift: Annotated[float, Field(ge=0, le=1)] | None


class IdentityReport(Contract):
    schema_version: Literal[1]
    status: Literal['pass', 'review', 'fail', 'not_measurable']
    faces: Annotated[list[IdentityFaceMetric], Field(max_length=100)]
    changed_mask: ArtifactRef
    generated_mask: ArtifactRef | None
    outside_mask_max_error: Annotated[float, Field(ge=0, le=1_000_000)]
    calibration: ArtifactRef

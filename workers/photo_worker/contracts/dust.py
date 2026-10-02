from __future__ import annotations

from typing import Annotated, Literal
from pydantic import Field, model_validator

from .base import ArtifactRef, Contract, SHA256, Unit


class SensorCrop(Contract):
    x: Annotated[int, Field(ge=0)]
    y: Annotated[int, Field(ge=0)]
    width: Annotated[int, Field(ge=8)]
    height: Annotated[int, Field(ge=8)]


class DustParams(Contract):
    frames: Annotated[list[ArtifactRef], Field(min_length=5, max_length=64)]
    camera_id: Annotated[str, Field(pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$")]
    sensor_width: Annotated[int, Field(ge=8, le=200_000)]
    sensor_height: Annotated[int, Field(ge=8, le=200_000)]
    raw_crop: SensorCrop
    orientation: Literal[1]
    recurrence: Annotated[float, Field(ge=.8, le=1)] = .8

    @model_validator(mode="after")
    def mapping(self):
        if self.raw_crop.x + self.raw_crop.width > self.sensor_width or self.raw_crop.y + self.raw_crop.height > self.sensor_height:
            raise ValueError("Raw crop exceeds sensor")
        if len({ref.artifact_id for ref in self.frames}) != len(self.frames):
            raise ValueError("Dust frames must be unique")
        return self


class DustCandidate(Contract):
    id: SHA256
    sensor_x: Annotated[float, Field(ge=0)]
    sensor_y: Annotated[float, Field(ge=0)]
    radius_sensor_px: Annotated[float, Field(gt=0, le=32)]
    observed_frames: Annotated[int, Field(ge=1, le=64)]
    eligible_frames: Annotated[int, Field(ge=1, le=64)]
    recurrence: Unit
    decision: Literal["pending"] = "pending"


class DustCandidates(Contract):
    frames: Annotated[list[ArtifactRef], Field(min_length=5, max_length=64)]
    camera_id: str
    sensor_width: int
    sensor_height: int
    raw_crop: SensorCrop
    algorithm: Literal["sensor_dust_log_v1"] = "sensor_dust_log_v1"
    candidates: Annotated[list[DustCandidate], Field(max_length=4096)]
    warnings: list[Literal["no_repeated_dust"]] = Field(max_length=1)


class DustReview(Contract):
    version: Literal["sensor_dust_review_v1"]
    proposal: ArtifactRef
    base: ArtifactRef
    revision: Annotated[int, Field(ge=1, le=9_007_199_254_740_991)]
    accepted_candidate_ids: Annotated[list[SHA256], Field(max_length=4096)]
    rejected_candidate_ids: Annotated[list[SHA256], Field(max_length=4096)]

    @model_validator(mode="after")
    def disjoint_selection(self):
        accepted, rejected = self.accepted_candidate_ids, self.rejected_candidate_ids
        if accepted != sorted(set(accepted)) or rejected != sorted(set(rejected)) or set(accepted) & set(rejected):
            raise ValueError("Dust review selection must be sorted, unique and disjoint")
        return self


class DustRepairParams(Contract):
    image: ArtifactRef
    proposal: ArtifactRef | None = None
    review: DustReview | None = None

    @model_validator(mode="after")
    def complete_authorization(self):
        if (self.proposal is None) != (self.review is None):
            raise ValueError("Dust proposal and review must be supplied together")
        return self


class DustRepairEvidence(Contract):
    candidate_id: SHA256
    center_x: float
    center_y: float
    radius: Annotated[float, Field(gt=0)]
    applied_pixels: Annotated[int, Field(ge=0)]


class DustRepairReport(Contract):
    image: ArtifactRef
    proposal: ArtifactRef | None
    review_revision: int
    candidates: Annotated[list[DustRepairEvidence], Field(max_length=4096)]
    warnings: list[Literal["no_accepted_candidates"]]


def accepted_dust_candidates(params: DustRepairParams, proposal: DustCandidates | None,
                             review: DustReview | None = None) -> list[DustCandidate]:
    review = params.review if review is None else review
    if params.proposal is None and review is None:
        return []
    if params.proposal is None or review is None:
        raise ValueError("STALE_DUST_REVIEW")
    if proposal is None:
        raise ValueError("STALE_DUST_REVIEW")
    if review.proposal != params.proposal or params.image not in proposal.frames:
        raise ValueError("STALE_DUST_REVIEW")
    known = {candidate.id: candidate for candidate in proposal.candidates}
    selected = []
    for candidate_id in review.accepted_candidate_ids:
        candidate = known.get(candidate_id)
        if candidate is None:
            raise ValueError("UNKNOWN_DUST_CANDIDATE")
        selected.append(candidate)
    if any(candidate_id not in known for candidate_id in review.rejected_candidate_ids):
        raise ValueError("UNKNOWN_DUST_CANDIDATE")
    return selected

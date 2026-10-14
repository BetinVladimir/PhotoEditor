from __future__ import annotations

from typing import Annotated, Literal
from pydantic import Field, model_validator

from .base import ArtifactRef, Contract, SHA256, Unit, canonical_bytes, canonical_hash


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
    sensor_x: Annotated[float, Field(ge=0, allow_inf_nan=False)]
    sensor_y: Annotated[float, Field(ge=0, allow_inf_nan=False)]
    radius_sensor_px: Annotated[float, Field(gt=0, le=32, allow_inf_nan=False)]
    observed_frames: Annotated[int, Field(ge=1, le=64)]
    eligible_frames: Annotated[int, Field(ge=1, le=64)]
    recurrence: Unit
    decision: Literal["pending"] = "pending"

    @model_validator(mode="after")
    def coherent_observations(self):
        if (self.observed_frames>self.eligible_frames
                or abs(self.recurrence-self.observed_frames/self.eligible_frames)>1e-12):
            raise ValueError("DUST_RECURRENCE_INVALID")
        return self


class DustCandidates(Contract):
    frames: Annotated[list[ArtifactRef], Field(min_length=5, max_length=64)]
    camera_id: Annotated[str, Field(pattern=r"^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$")]
    sensor_width: Annotated[int, Field(ge=8, le=200_000)]
    sensor_height: Annotated[int, Field(ge=8, le=200_000)]
    raw_crop: SensorCrop
    algorithm: Literal["sensor_dust_log_v1"] = "sensor_dust_log_v1"
    candidates: Annotated[list[DustCandidate], Field(max_length=4096)]
    warnings: list[Literal["no_repeated_dust"]] = Field(max_length=1)

    @model_validator(mode="after")
    def coherent_series(self):
        crop=self.raw_crop
        if (crop.x+crop.width>self.sensor_width or crop.y+crop.height>self.sensor_height
                or len({ref.artifact_id for ref in self.frames})!=len(self.frames)
                or len({candidate.id for candidate in self.candidates})!=len(self.candidates)
                or self.warnings!=([] if self.candidates else ["no_repeated_dust"])
                or any(candidate.eligible_frames!=len(self.frames) or candidate.recurrence<.8
                    or not crop.x<=candidate.sensor_x<crop.x+crop.width
                    or not crop.y<=candidate.sensor_y<crop.y+crop.height
                    for candidate in self.candidates)):
            raise ValueError("DUST_SERIES_INVALID")
        return self


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
    center_x: Annotated[float, Field(allow_inf_nan=False)]
    center_y: Annotated[float, Field(allow_inf_nan=False)]
    radius: Annotated[float, Field(gt=0, le=200_000_000, allow_inf_nan=False)]
    applied_pixels: Annotated[int, Field(ge=0, le=200_000_000)]


class DustRepairReport(Contract):
    image: ArtifactRef
    proposal: ArtifactRef | None
    review_revision: Annotated[int, Field(ge=1, le=9_007_199_254_740_991)]
    candidates: Annotated[list[DustRepairEvidence], Field(max_length=4096)]
    warnings: list[Literal["no_accepted_candidates"]] = Field(max_length=1)

    @model_validator(mode="after")
    def coherent_repair(self):
        if (len({item.candidate_id for item in self.candidates})!=len(self.candidates)
                or self.candidates and self.proposal is None
                or self.warnings!=([] if self.candidates else ["no_accepted_candidates"])):
            raise ValueError("DUST_REPAIR_EVIDENCE_INVALID")
        return self


def accepted_dust_candidates(params: DustRepairParams, proposal: DustCandidates | None,
                             review: DustReview | None = None) -> list[DustCandidate]:
    # Pydantic models are mutable and model_copy(update=...) skips validation.
    # Revalidate owned snapshots at the actual repair authorization boundary.
    params = DustRepairParams.model_validate_json(canonical_bytes(params))
    if review is not None:
        review = DustReview.model_validate_json(canonical_bytes(review))
        if params.review != review:
            raise ValueError("STALE_DUST_REVIEW")
    review = params.review if review is None else review
    if proposal is not None:
        proposal = verify_dust_candidate_ids(proposal)
    if params.proposal is None and review is None:
        return []
    if params.proposal is None or review is None:
        raise ValueError("STALE_DUST_REVIEW")
    if proposal is None:
        raise ValueError("STALE_DUST_REVIEW")
    if (review.proposal != params.proposal or params.image not in proposal.frames
            or review.base not in proposal.frames):
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


def dust_candidate_id(camera_id,sensor_width,sensor_height,frames,x,y,radius):
    """Canonical sensor_dust_log_v1 identity shared with backend review.

    Frame order is significant. Crop/provenance validation is performed by
    DustCandidates and the scoped backend admission, not inferred from a hash.
    """
    identity=dict(algorithm='sensor_dust_log_v1',camera_id=camera_id,
        sensor=[sensor_width,sensor_height],x=x,y=y,radius=radius,
        frames=[ref.sha256 for ref in frames])
    return canonical_hash(identity)


def verify_dust_candidate_ids(proposal):
    """Revalidate an owned proposal and reject substituted candidate identities."""
    proposal=DustCandidates.model_validate_json(canonical_bytes(proposal))
    for candidate in proposal.candidates:
        expected=dust_candidate_id(proposal.camera_id,proposal.sensor_width,proposal.sensor_height,
            proposal.frames,candidate.sensor_x,candidate.sensor_y,candidate.radius_sensor_px)
        if candidate.id!=expected:raise ValueError('DUST_CANDIDATE_ID_INVALID')
    return proposal

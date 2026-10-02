from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef


class BodyFitParams(Contract):
    pair_id:UUID
    before:ArtifactRef
    after:ArtifactRef
    alignment:ArtifactRef
    before_pose:ArtifactRef
    after_pose:ArtifactRef
    before_person:MaskRef
    after_person:MaskRef
    before_faces:ArtifactRef|None=None
    after_faces:ArtifactRef|None=None
    algorithm:Literal['bodywarp_v1']='bodywarp_v1'
    min_confidence:Literal[.75]=.75
    evidence_mode:Literal['fitted']='fitted'

    @model_validator(mode='after')
    def paired_faces(self):
        if (self.before_faces is None)!=(self.after_faces is None):raise ValueError('Face evidence must be paired')
        if self.before_person.selector!='person' or self.after_person.selector!='person':raise ValueError('Body fit requires person masks')
        return self


class BodyCoefficients(Contract):
    face_slim:Annotated[float,Field(ge=0,le=.05)]
    waist_slim:Annotated[float,Field(ge=0,le=.08)]
    hip_slim:Annotated[float,Field(ge=0,le=.08)]
    torso_slim:Annotated[float,Field(ge=0,le=.08)]
    leg_length:Annotated[float,Field(ge=0,le=.03)]


class BodyFitMetrics(Contract):
    iou:Unit
    landmark_error:Annotated[float,Field(ge=0,le=1)]
    silhouette_error:Annotated[float,Field(ge=0,le=1)]
    background_anchor_error:Annotated[float,Field(ge=0,le=1)]


class BodyObjectFit(Contract):
    before_object_id:UUID
    after_object_id:UUID
    confidence:Unit
    match_cost:Unit
    ambiguity_margin:Unit
    requested:BodyCoefficients
    applied:BodyCoefficients
    metrics:BodyFitMetrics
    accepted:bool
    reason:Literal['applied','ambiguous','low_confidence','missing_anchors','sitting_or_occluded']

    @model_validator(mode='after')
    def safe_skip(self):
        if not self.accepted and any(self.applied.model_dump().values()):raise ValueError('Skipped fit must apply identity')
        if self.accepted and self.reason!='applied':raise ValueError('Accepted fit reason mismatch')
        return self


class BodyFit(Contract):
    params:BodyFitParams
    objects:Annotated[list[BodyObjectFit],Field(max_length=100)]
    algorithm:Literal['bodywarp_v1']='bodywarp_v1'
    evidence_mode:Literal['fitted']='fitted'
    warnings:Annotated[list[Literal['no_people','unmatched_people','ambiguous_match','low_confidence','face_evidence_unavailable']],Field(max_length=5)]

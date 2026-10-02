from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef
from .body_fit import BodyCoefficients

class BodyParams(Contract):
    image:ArtifactRef
    poses:ArtifactRef
    person:MaskRef
    fit:ArtifactRef|None=None
    coefficients:BodyCoefficients|None=None
    faces:ArtifactRef|None=None
    strength:Unit=1
    algorithm:Literal['bodywarp_v1']='bodywarp_v1'
    protect_background:Literal[True]=True
    max_displacement_image_fraction:Annotated[float,Field(gt=0,le=.02)]=.02
    on_unsafe:Literal['skip_module']='skip_module'

    @model_validator(mode='after')
    def valid_person(self):
        if self.person.selector!='person':raise ValueError('Body warp requires person mask')
        if (self.fit is None)==(self.coefficients is None):raise ValueError('Select exactly one body parameter source')
        return self


class BodyWarpMetrics(Contract):
    max_displacement_pixels:Annotated[float,Field(ge=0)]
    min_jacobian:Annotated[float,Field(ge=-1e6,le=1e6)]
    protected_center_displacement_pixels:Annotated[float,Field(ge=0)]


class BodyWarpReport(Contract):
    image:ArtifactRef
    poses:ArtifactRef
    person:ArtifactRef
    faces:ArtifactRef|None
    requested_strength:Unit
    effective_strength:Unit
    attempts:Annotated[int,Field(ge=0,le=4)]
    applied:bool
    reason:Literal['applied','zero_strength','no_people','unsafe','ambiguous_overlap']
    metrics:BodyWarpMetrics
    object_ids:Annotated[list[UUID],Field(max_length=100)]
    warnings:Annotated[list[Literal['no_people','unsafe_identity','overlapping_people']],Field(max_length=3)]

    @model_validator(mode='after')
    def identity_reason(self):
        if self.applied!=(self.reason=='applied'):raise ValueError('Body warp report state mismatch')
        if not self.applied and self.effective_strength!=0:raise ValueError('Skipped body warp must be identity')
        return self

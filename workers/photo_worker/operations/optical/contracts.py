from typing import Annotated, Literal
from pydantic import Field, model_validator, field_validator
from photo_worker.contracts.base import ArtifactRef, Contract, Unit

Coefficient = Annotated[float, Field(ge=-2, le=2)]

class LensProfile(Contract):
    schema_version: Literal[1]
    camera_model: Annotated[str, Field(min_length=1, max_length=120)]
    lens_model: Annotated[str, Field(min_length=1, max_length=120)]
    focal_mm: Annotated[float, Field(ge=1, le=2000)]
    aperture: Annotated[float, Field(ge=.5, le=128)]
    cx: Unit
    cy: Unit
    k: Annotated[list[Coefficient], Field(min_length=3, max_length=3)]
    p: Annotated[list[Annotated[float, Field(ge=-.1, le=.1)]], Field(min_length=2, max_length=2)]
    vignette: Annotated[list[Coefficient], Field(min_length=3, max_length=3)]
    ca_red_scale: Annotated[float, Field(ge=.99, le=1.01)]
    ca_blue_scale: Annotated[float, Field(ge=.99, le=1.01)]
    qualification: ArtifactRef


from photo_worker.operations.session.contracts import VersionBinding


class OpticalLensParams(Contract):
    schema_version: Literal[1]
    base: VersionBinding
    strength: Unit
    profile: ArtifactRef
    focal_mm: Annotated[float, Field(ge=1, le=2000)]
    aperture: Annotated[float, Field(ge=.5, le=128)]


class ImageResult(Contract):
    schema_version: Literal[1]
    working_image: ArtifactRef
    report: ArtifactRef
    changed_mask: ArtifactRef
    generated_mask: ArtifactRef | None


class ImageReport(Contract):
    schema_version: Literal[1]
    status: Literal['pass', 'review', 'fail', 'unavailable']
    operation: Annotated[str, Field(min_length=1, max_length=80)]
    skipped_count: Annotated[int, Field(ge=0, le=5000)]
    reason: Annotated[str, Field(min_length=1, max_length=200)] | None
    outside_mask_max_error: Annotated[float, Field(ge=0, le=1_000_000)]
    clipping_fraction: Unit
    identity: ArtifactRef | None
    transform: ArtifactRef | None
    sources: Annotated[list[VersionBinding], Field(min_length=1, max_length=30)]
    registrations: Annotated[list[ArtifactRef], Field(max_length=30)]

    parent_reports: Annotated[list[ArtifactRef], Field(max_length=33)] | None = Field(default=None,exclude_if=lambda value:value is None)

    @field_validator('parent_reports',mode='before')
    @classmethod
    def valid_parent_reports(cls,value):
        if not isinstance(value,list):raise ValueError('IMAGE_REPORT_PARENTS_INVALID')
        return value

    @model_validator(mode='after')
    def unique_parent_reports(self):
        if self.parent_reports is not None:
            ids=[ref.artifact_id for ref in self.parent_reports]
            if len(set(ids))!=len(ids) or any(ref.artifact_id.int==0 or ref.sha256=='0'*64 for ref in self.parent_reports):
                raise ValueError('IMAGE_REPORT_PARENTS_INVALID')
        return self

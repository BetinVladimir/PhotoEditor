from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from .base import Contract, ArtifactRef, Unit
from .images import MaskRef, WorkingImage

RegionId = Annotated[str, Field(min_length=1,max_length=120)]

class RegionMask(Contract):
    region_id: RegionId
    mask: MaskRef

class LocalParams(Contract):
    item_id: UUID | None = None
    image: ArtifactRef
    base_aligned: ArtifactRef
    resolved: ArtifactRef
    series: ArtifactRef | None = None
    person: MaskRef | None = None
    regions: Annotated[list[RegionMask],Field(max_length=128)] = Field(default_factory=list)

    @model_validator(mode='after')
    def unique_regions(self):
        if len({r.region_id for r in self.regions}) != len(self.regions):
            raise ValueError('Duplicate region binding')
        if self.person is not None and self.person.selector != 'person':
            raise ValueError('Expected person selector')
        return self

    def validate_geometry(self, image: WorkingImage, base: WorkingImage):
        if (image.dtype != 'float32' or base.dtype != 'float32'
            or image.color.profile_id != 'work_rec2020_gamma24_v1'
            or image.color != base.color or image.canvas != base.canvas
            or image.transform_chain != base.transform_chain or image.alpha != base.alpha):
            raise ValueError('Local image and aligned base disagree')
        for mask in [self.person, *(r.mask for r in self.regions)]:
            if mask is not None and (mask.canvas != image.canvas or mask.transform_chain != image.transform_chain):
                raise ValueError('Mask geometry mismatch')

class RegionSkip(Contract):
    region_id: RegionId
    reason: Literal['missing_mask','selector_mismatch','mask_unreliable']

class LocalClipping(Contract):
    stage: Literal["series","local","regions"]
    below_zero: Unit
    above_one: Unit

class LocalReport(Contract):
    image: ArtifactRef
    base_aligned: ArtifactRef
    resolved: ArtifactRef
    series: ArtifactRef | None = None
    series_applied: bool = False
    series_reason: str | None = None
    pixel_count: Annotated[int,Field(ge=1,le=200000000)]
    strength: Unit
    strength_applied: Literal[True] = True
    local_applied: bool
    local_skip: Literal['disabled','missing_mask','mask_unreliable'] | None
    region_skips: Annotated[list[RegionSkip],Field(max_length=128)]
    clipping: Annotated[list[LocalClipping],Field(max_length=2)]
    region_algorithm: Literal['linear_rec2020_regions_v1'] = 'linear_rec2020_regions_v1'

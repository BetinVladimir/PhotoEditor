from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef,NormalizedRect


class LineSegment(Contract):
    x0:Unit;y0:Unit;x1:Unit;y1:Unit
    confidence:Unit=1


class ProtectedPoint(Contract):
    x:Unit;y:Unit
    kind:Literal['face','eye','head','person','manual']
    confidence:Unit=1


class ImageStructure(Contract):
    image:ArtifactRef
    lines:Annotated[list[LineSegment],Field(max_length=4096)]=Field(default_factory=list)
    protected_points:Annotated[list[ProtectedPoint],Field(max_length=1024)]=Field(default_factory=list)
    subjects:Annotated[list[NormalizedRect],Field(max_length=128)]=Field(default_factory=list)


class StructureParams(Contract):
    image:ArtifactRef
    subject:MaskRef|None=None
    max_lines:Annotated[int,Field(ge=1,le=4096)]=512
    canny_low:Annotated[int,Field(ge=1,le=254)]=50
    canny_high:Annotated[int,Field(ge=2,le=255)]=150

    @model_validator(mode='after')
    def thresholds(self):
        if self.canny_low>=self.canny_high:raise ValueError('Canny thresholds must increase')
        return self


class CompositionParams(Contract):
    structure:ArtifactRef
    aspect_width:Annotated[int,Field(ge=1,le=10000)]
    aspect_height:Annotated[int,Field(ge=1,le=10000)]
    headroom_target:Annotated[float,Field(ge=.01,le=.5)]=.12
    allow_rotation:bool=True
    max_rotation_degrees:Annotated[float,Field(ge=0,le=5)]=5.0
    max_perspective_area_change:Literal[.15]=.15


class CompositionPlan(Contract):
    image:ArtifactRef
    structure:ArtifactRef
    source_width:Annotated[int,Field(ge=1)]
    source_height:Annotated[int,Field(ge=1)]
    output_width:Annotated[int,Field(ge=1)]
    output_height:Annotated[int,Field(ge=1)]
    forward_matrix:Annotated[list[float],Field(min_length=9,max_length=9)]
    source_quad:Annotated[list[Annotated[float,Field(ge=-1e9,le=1e9)]],Field(min_length=8,max_length=8)]
    rotation_degrees:Annotated[float,Field(ge=-5,le=5)]
    perspective_area_change:Annotated[float,Field(ge=0,le=.15)]
    retained_subject_fraction:Unit
    center_offset:Unit
    protected_points_preserved:bool
    disposition:Literal['apply','review','outpaint']
    reasons:Annotated[list[Literal['large_rotation','no_valid_crop','subject_loss','no_structure']],Field(max_length=4)]
    outpaint_side:Literal['left','right','top','bottom']|None=None
    outpaint_pixels:Annotated[int,Field(ge=8,le=8192)]|None=None

    @model_validator(mode='after')
    def coherent(self):
        if self.disposition=='apply' and not self.protected_points_preserved:raise ValueError('Apply plan loses protected points')
        if self.disposition=='outpaint' and 'no_valid_crop' not in self.reasons:raise ValueError('Outpaint requires no-valid-crop reason')
        if self.disposition=='outpaint' and (self.outpaint_side is None or self.outpaint_pixels is None):raise ValueError('Outpaint geometry missing')
        if self.disposition!='outpaint' and (self.outpaint_side is not None or self.outpaint_pixels is not None):raise ValueError('Unexpected outpaint geometry')
        return self


class CropParams(Contract):
    image:ArtifactRef
    plan:ArtifactRef
    interpolation:Literal['lanczos4']='lanczos4'


class CropReport(Contract):
    image:ArtifactRef
    plan:ArtifactRef
    applied:bool
    output_width:Annotated[int,Field(ge=1)]
    output_height:Annotated[int,Field(ge=1)]
    transform_count:Literal[1]
    interpolation:Literal['lanczos4']
    alpha_preserved:bool
    review_reasons:Annotated[list[str],Field(max_length=8)]

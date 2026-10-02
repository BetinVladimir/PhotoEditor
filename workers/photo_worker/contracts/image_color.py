from typing import Annotated,Literal
from pydantic import Field
from .base import Contract,ArtifactRef,Unit

class ColorParams(Contract):
    image:ArtifactRef
    resolved:ArtifactRef

class ClippingStage(Contract):
    stage:Literal['gains','saturation','residual_lut','absolute_lut']
    below_zero:Unit
    above_one:Unit

class ColorReport(Contract):
    image:ArtifactRef
    resolved:ArtifactRef
    pixel_count:Annotated[int,Field(ge=1,le=200000000)]
    clipping:Annotated[list[ClippingStage],Field(min_length=1,max_length=3)]
    strength_applied:Literal[False]=False

from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import Canvas

class MaskPoint(Contract):
    x:Unit
    y:Unit

class Stroke(Contract):
    kind:Literal['stroke']
    sequence:Annotated[int,Field(ge=0,le=2**31-1)]
    mode:Literal['add','subtract','protect']
    points:Annotated[list[MaskPoint],Field(min_length=1,max_length=4096)]
    radius:Annotated[float,Field(gt=0,le=1)]
    hardness:Unit

class Polygon(Contract):
    kind:Literal['polygon']
    sequence:Annotated[int,Field(ge=0,le=2**31-1)]
    mode:Literal['add','subtract','protect']
    points:Annotated[list[MaskPoint],Field(min_length=3,max_length=4096)]

Shape=Annotated[Stroke|Polygon,Field(discriminator='kind')]

class ManualMask(Contract):
    version:Literal['manual_mask_v1']='manual_mask_v1'
    source_sha256:SHA256
    canvas:Canvas
    shapes:Annotated[list[Shape],Field(max_length=1024)]

    @model_validator(mode='after')
    def coherent(self):
        if self.source_sha256!=self.canvas.source_sha256:
            raise ValueError('Manual mask source mismatch')
        if len({s.sequence for s in self.shapes})!=len(self.shapes):
            raise ValueError('Duplicate shape sequence')
        if sum(len(s.points) for s in self.shapes)>10000:
            raise ValueError('Too many vector points')
        if self.canvas.width*self.canvas.height>200000000:
            raise ValueError('Manual mask pixel limit')
        return self

from uuid import UUID
from .base import ArtifactRef
from .images import MaskRef

class MaskRasterParams(Contract):
    image:ArtifactRef
    vectors:ArtifactRef|None=None
    inline_vectors:ManualMask|None=None
    object_id:UUID|None=None

    @model_validator(mode='after')
    def source(self):
        if (self.vectors is None)==(self.inline_vectors is None):raise ValueError('Exactly one vector source is required')
        return self

class RasterizedMask(Contract):
    image:ArtifactRef
    vectors:ArtifactRef
    target:MaskRef
    protected:MaskRef
    target_png:ArtifactRef
    protected_png:ArtifactRef
    algorithm:Literal['manual_mask_v1']='manual_mask_v1'

    @model_validator(mode='after')
    def distinct_output_artifacts(self):
        refs=(self.target.artifact,self.protected.artifact,self.target_png,self.protected_png)
        if len({ref.artifact_id for ref in refs})!=4:
            raise ValueError('Rasterized mask outputs must have distinct artifact IDs')
        return self

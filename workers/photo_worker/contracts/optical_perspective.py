"""Strict authored perspective intent and normalized line evidence."""
from typing import Annotated,Literal
from pydantic import Field,model_validator
from photo_worker.contracts.base import Contract,ArtifactRef,Unit
from photo_worker.operations.session.contracts import VersionBinding


class PerspectivePoint(Contract):
    x:Unit
    y:Unit


class PerspectiveLine(Contract):
    a:PerspectivePoint
    b:PerspectivePoint
    family:Literal['horizontal','vertical']

    @model_validator(mode='after')
    def nonzero_length(self):
        if self.a==self.b:raise ValueError('PERSPECTIVE_LINES_INVALID')
        return self


class LineSegments(Contract):
    schema_version:Literal[1]
    lines:Annotated[list[PerspectiveLine],Field(min_length=4,max_length=1000)]

    @model_validator(mode='after')
    def families(self):
        for family in ('horizontal','vertical'):
            if sum(line.family==family for line in self.lines)<2:
                raise ValueError('PERSPECTIVE_LINE_FAMILY_MISSING')
        return self

    def pixel_families(self,width:int,height:int):
        if any(type(v) is not int or v<1 for v in (width,height)):
            raise ValueError('PERSPECTIVE_LIMIT_INVALID')
        def family(name):
            return [[[line.a.x*width,line.a.y*height],[line.b.x*width,line.b.y*height]]
                    for line in self.lines if line.family==name]
        return family('horizontal'),family('vertical')


class OpticalPerspectiveParams(Contract):
    schema_version:Literal[1]
    base:VersionBinding
    strength:Unit
    mode:Literal['auto','manual']
    corners:Annotated[list[PerspectivePoint],Field(min_length=4,max_length=4)]|None
    lines:ArtifactRef|None
    max_crop_fraction:Unit

    @model_validator(mode='after')
    def mode_inputs(self):
        if self.mode=='manual':
            if self.corners is None or self.lines is not None:
                raise ValueError('PERSPECTIVE_MODE_INPUT_INVALID')
            points=[(p.x,p.y) for p in self.corners]
            edges=[(points[(i+1)%4][0]-points[i][0],points[(i+1)%4][1]-points[i][1]) for i in range(4)]
            if any(edges[i][0]*edges[(i+1)%4][1]-edges[i][1]*edges[(i+1)%4][0]<=0 for i in range(4)):
                raise ValueError('PERSPECTIVE_CORNERS_INVALID')
        elif self.lines is None or self.corners is not None:
            raise ValueError('PERSPECTIVE_MODE_INPUT_INVALID')
        return self

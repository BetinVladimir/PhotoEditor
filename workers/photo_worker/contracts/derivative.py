from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit


class ResizeParams(Contract):
    image:ArtifactRef
    long_edge:Annotated[int,Field(ge=1,le=100000)]|None=None
    width:Annotated[int,Field(ge=1,le=100000)]|None=None
    height:Annotated[int,Field(ge=1,le=100000)]|None=None
    fit:Literal['contain']='contain'
    allow_upscale:bool=False

    @model_validator(mode='after')
    def one_size(self):
        if (self.long_edge is None)==(self.width is None):raise ValueError('Specify long edge or box')
        if (self.width is None)!=(self.height is None):raise ValueError('Resize box requires width and height')
        return self


class SharpenParams(Contract):
    image:ArtifactRef
    radius:Annotated[float,Field(ge=.1,le=5)]
    strength:Unit
    threshold:Annotated[float,Field(ge=0,le=.1)]
    max_delta:Annotated[float,Field(gt=0,le=.03)]=.03
    algorithm:Literal['luma_unsharp_v1']='luma_unsharp_v1'


class WatermarkParams(Contract):
    image:ArtifactRef
    watermark:ArtifactRef
    opacity:Unit
    anchor:Literal['top_left','top_right','bottom_left','bottom_right','center']
    margin_x:Unit=.03
    margin_y:Unit=.03
    max_width_fraction:Annotated[float,Field(gt=0,le=1)]=.25
    algorithm:Literal['linear_alpha_v1']='linear_alpha_v1'


class DerivativeReport(Contract):
    image:ArtifactRef
    operation:Literal['resize','sharpen','watermark']
    applied:bool
    width:Annotated[int,Field(ge=1,le=200000)]
    height:Annotated[int,Field(ge=1,le=200000)]
    max_channel_delta:Annotated[float,Field(ge=0,le=1)]
    warnings:list[Literal['no_resize_required','zero_strength','zero_opacity']]

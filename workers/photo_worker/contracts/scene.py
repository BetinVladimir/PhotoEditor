from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256


class SceneParams(Contract):
    image:ArtifactRef
    model_id:Literal['dinov2_vits14']='dinov2_vits14'
    model_lock_id:SHA256
    preprocessing_id:Literal['dinov2_pad518_v1']='dinov2_pad518_v1'
    input_size:Literal[518]=518


class Embedding384(Contract):
    image:ArtifactRef
    values:Annotated[list[float],Field(min_length=384,max_length=384)]
    model_id:Literal['dinov2_vits14']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['dinov2_pad518_v1']
    source_clip_fraction:Annotated[float,Field(ge=0,le=1)]

    @model_validator(mode='after')
    def normalized(self):
        norm=sum(x*x for x in self.values)**.5
        if not .999<=norm<=1.001:raise ValueError('Embedding must be L2 normalized')
        return self

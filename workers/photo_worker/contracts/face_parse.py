from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import Canvas,TransformChain


class FaceParseParams(Contract):
    image:ArtifactRef
    faces:ArtifactRef
    model_id:Literal['bisenet_face19']='bisenet_face19'
    model_lock_id:SHA256
    preprocessing_id:Literal['bisenet_face19_crop512_v1']='bisenet_face19_crop512_v1'
    crop_size:Literal[512]=512
    expansion:Literal[1.35]=1.35


class FaceLabels19(Contract):
    image:ArtifactRef
    faces:ArtifactRef
    labels:ArtifactRef
    canvas:Canvas
    transform_chain:TransformChain
    classes:Literal[19]=19
    model_id:Literal['bisenet_face19']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['bisenet_face19_crop512_v1']
    parsed_faces:Annotated[int,Field(ge=0,le=100)]
    mean_confidence:Unit
    warnings:Annotated[list[Literal['no_faces','low_confidence','overlapping_faces','source_srgb_clipped']],Field(max_length=4)]

    @model_validator(mode='after')
    def geometry(self):self.transform_chain.validate_target(self.canvas);return self

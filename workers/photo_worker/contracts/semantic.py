from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import Canvas,MaskRef,TransformChain


class SceneParseParams(Contract):
    image:ArtifactRef
    model_id:Literal['segformer_b0_ade']='segformer_b0_ade'
    model_lock_id:SHA256
    preprocessing_id:Literal['segformer_ade512_official_v1']='segformer_ade512_official_v1'


class SceneLabels(Contract):
    image:ArtifactRef
    labels:ArtifactRef
    canvas:Canvas
    transform_chain:TransformChain
    classes:Literal[150]=150
    model_id:Literal['segformer_b0_ade']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['segformer_ade512_official_v1']
    confidence:Unit
    warnings:Annotated[list[Literal['low_confidence','source_srgb_clipped']],Field(max_length=2)]

    @model_validator(mode='after')
    def geometry(self):self.transform_chain.validate_target(self.canvas);return self


class AlphaParams(Contract):
    image:ArtifactRef
    model_id:Literal['u2net']='u2net'
    model_lock_id:SHA256
    preprocessing_id:Literal['u2net_320_v1']='u2net_320_v1'


class AlphaPrediction(Contract):
    image:ArtifactRef
    alpha:MaskRef
    preview_width:Literal[320]=320
    preview_height:Literal[320]=320
    model_id:Literal['u2net']
    model_lock_id:SHA256
    backend:Literal['cpu']
    preprocessing_id:Literal['u2net_320_v1']
    foreground_fraction:Unit
    source_clip_fraction:Unit
    warnings:Annotated[list[Literal['empty_foreground','low_confidence','source_srgb_clipped']],Field(max_length=3)]

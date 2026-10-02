from typing import Annotated,Literal
from pydantic import Field
from .base import ArtifactRef,Contract,SHA256,Unit


class PersonParams(Contract):
    image:ArtifactRef
    model_id:Literal['lraspp_mobilenet_v3_large']='lraspp_mobilenet_v3_large'
    model_lock_id:SHA256
    preprocessing_id:Literal['person_guided_v1']='person_guided_v1'
    long_side:Literal[768]=768
    guided_tile:Literal[256,512,1024]=512


class PersonReport(Contract):
    image:ArtifactRef
    mask:ArtifactRef
    preview_width:Annotated[int,Field(ge=1,le=768)]
    preview_height:Annotated[int,Field(ge=1,le=768)]
    model_id:Literal['lraspp_mobilenet_v3_large']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['person_guided_v1']
    person_class:Literal[15]=15
    confidence:Unit
    person_fraction:Unit
    source_clip_fraction:Unit
    alpha_excluded_fraction:Unit
    warnings:Annotated[list[Literal['low_confidence','empty_person','source_srgb_clipped']],Field(max_length=3)]

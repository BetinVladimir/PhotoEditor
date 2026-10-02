from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit
from .images import MaskRef,NormalizedRect


class PromptPoint(Contract):
    x:Unit;y:Unit
    label:Literal['positive','negative']


class PromptMaskParams(Contract):
    image:ArtifactRef
    source_sha256:SHA256
    object_id:UUID|None=None
    selector:Annotated[str,Field(pattern=r'^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$')]='manual'
    points:Annotated[list[PromptPoint],Field(min_length=1,max_length=128)]
    box:NormalizedRect|None=None
    model_id:Literal['mobilesam']='mobilesam'
    model_lock_id:SHA256
    preprocessing_id:Literal['mobilesam_long1024_v1']='mobilesam_long1024_v1'

    @model_validator(mode='after')
    def positive(self):
        if not any(p.label=='positive' for p in self.points):raise ValueError('At least one positive point required')
        return self


class PromptMaskReport(Contract):
    image:ArtifactRef
    mask:MaskRef
    candidate_count:Annotated[int,Field(ge=1,le=16)]
    selected_index:Annotated[int,Field(ge=0,le=15)]
    score:Unit
    model_id:Literal['mobilesam']
    model_lock_id:SHA256
    backend:Literal['cpu','cuda','mps']
    preprocessing_id:Literal['mobilesam_long1024_v1']
    warnings:Annotated[list[Literal['low_confidence','empty_mask']],Field(max_length=2)]

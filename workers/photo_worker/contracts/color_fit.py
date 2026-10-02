from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import Contract,ArtifactRef,SHA256,UInt32
from .images import MaskRef
from .style import GlobalColor,LocalModule

class ColorFitParams(Contract):
    pair_id:UUID
    before:ArtifactRef
    after:ArtifactRef
    alignment:ArtifactRef
    person:MaskRef|None=None
    ignore:MaskRef|None=None
    seed:UInt32=42
    samples:Literal[65536]=65536
    local_requested:bool=True
    geometry_mode:Literal['similarity_only']='similarity_only'

    @model_validator(mode='after')
    def person_selector(self):
        if self.person is not None and self.person.selector!='person':raise ValueError('Local fit requires person mask')
        if self.ignore is not None and self.ignore.selector!='ignore':raise ValueError('Ignore mask requires ignore selector')
        return self

class FitSamplingEvidence(Contract):
    split_digest:SHA256
    holdout_blocks:Annotated[list[Annotated[int,Field(ge=0,le=63)]],Field(min_length=1,max_length=63)]
    train_digest:SHA256
    holdout_digest:SHA256
    train_seed:UInt32
    holdout_seed:UInt32
    warp_version:Literal['linear_bilinear_global_v1']
    train_count:Annotated[int,Field(ge=1,le=65536)]
    holdout_count:Annotated[int,Field(ge=1,le=65536)]

    @model_validator(mode='after')
    def consistent(self):
        if self.holdout_blocks!=sorted(set(self.holdout_blocks)):raise ValueError('Invalid holdout blocks')
        if self.holdout_seed!=(self.train_seed+1)%(2**32):raise ValueError('Invalid holdout seed')
        return self

class FitCheckpoint(Contract):
    stage:Literal['identity','parameters','residual','local']
    step:Annotated[int,Field(ge=0,le=600)]
    holdout_loss:Annotated[float,Field(ge=0)]

class ColorFit(Contract):
    params:ColorFitParams
    global_color:GlobalColor
    local:LocalModule
    holdout_loss:Annotated[float,Field(ge=0)]
    median_delta_e:Annotated[float,Field(ge=0)]
    p95_delta_e:Annotated[float,Field(ge=0)]
    initialization_index:Annotated[int,Field(ge=0,le=2)]
    optimizer_version:Literal['color_fit_adam_v1']
    backend:Literal['cpu','mps','cuda']
    sampling:FitSamplingEvidence
    history:Annotated[list[FitCheckpoint],Field(min_length=1,max_length=1500)]
    transfer_confidence:Literal['unvalidated']='unvalidated'
    warnings:Annotated[list[Literal['alignment_review_required','local_mask_unavailable','local_mask_unreliable']],Field(max_length=3)]

    @model_validator(mode='after')
    def consistent(self):
        if self.p95_delta_e<self.median_delta_e:raise ValueError('Invalid reconstruction quantiles')
        if self.sampling.train_seed!=self.params.seed:raise ValueError('Sampling seed mismatch')
        if self.local.enabled and (not self.params.local_requested or self.params.person is None):raise ValueError('Local fit lacks requested mask')
        if len(self.warnings)!=len(set(self.warnings)):raise ValueError('Duplicate fit warnings')
        return self

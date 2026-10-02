from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef


class DetailRegion(Contract):
    region_id:Annotated[str,Field(pattern=r'^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$')]
    kind:Literal['teeth','sclera','shine','under_eye','cloth','glasses']
    mask:MaskRef
    confirmed:bool
    strength:Unit=1

    @model_validator(mode='after')
    def role(self):
        if self.mask.protected or self.mask.selector not in ('manual','teeth','sclera','skin','clothing','glasses'):raise ValueError('Invalid portrait detail mask')
        if self.kind in ('cloth','glasses') and not self.confirmed:raise ValueError('Cloth/glasses require confirmation')
        return self


class PortraitParams(Contract):
    image:ArtifactRef
    resolved:ArtifactRef
    regions:Annotated[list[DetailRegion],Field(max_length=128)]
    protected:Annotated[list[MaskRef],Field(max_length=32)]=Field(default_factory=list)

    @model_validator(mode='after')
    def coherent(self):
        if len({r.region_id for r in self.regions})!=len(self.regions):raise ValueError('Duplicate portrait region')
        if any(not m.protected for m in self.protected):raise ValueError('Expected protected masks')
        return self


class DetailEvidence(Contract):
    region_id:str
    kind:str
    applied:bool
    changed_pixels:Annotated[int,Field(ge=0,le=200000000)]
    max_ev:Annotated[float,Field(ge=0,le=.3)]
    chroma_reduction:Annotated[float,Field(ge=0,le=.25)]
    reason:Literal['zero_strength','empty_mask','review_required']|None


class PortraitDetailReport(Contract):
    image:ArtifactRef
    resolved:ArtifactRef
    regions:Annotated[list[DetailEvidence],Field(max_length=128)]
    alpha_preserved:bool
    warnings:Annotated[list[Literal['module_disabled','no_regions','region_skipped','glasses_generation_required']],Field(max_length=4)]
    algorithm:Literal['portrait_detail_v1']='portrait_detail_v1'

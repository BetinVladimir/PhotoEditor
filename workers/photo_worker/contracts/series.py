from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,Unit
from .images import MaskRef


class ComparableRegion(Contract):
    region_id:Annotated[str,Field(pattern=r'^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$')]
    mask:MaskRef


class SeriesMeasureParams(Contract):
    item_id:UUID
    image:ArtifactRef
    regions:Annotated[list[ComparableRegion],Field(min_length=1,max_length=32)]
    algorithm:Literal['seriesmatch_v1']='seriesmatch_v1'

    @model_validator(mode='after')
    def unique_regions(self):
        if len({r.region_id for r in self.regions})!=len(self.regions):raise ValueError('Duplicate comparable region')
        return self


class RegionStats(Contract):
    region_id:Annotated[str,Field(pattern=r'^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$')]
    mean_rgb:Annotated[list[Unit],Field(min_length=3,max_length=3)]
    mean_y:Annotated[float,Field(ge=0,le=1)]
    coverage:Unit
    confidence:Unit


class SeriesStats(Contract):
    item_id:UUID
    image:ArtifactRef
    regions:Annotated[list[RegionStats],Field(min_length=1,max_length=32)]
    clipping_fraction:Unit
    algorithm:Literal['seriesmatch_v1']='seriesmatch_v1'
    warnings:Annotated[list[Literal['low_region_coverage','low_mask_confidence']],Field(max_length=2)]

    @model_validator(mode='after')
    def unique_regions(self):
        if len({r.region_id for r in self.regions})!=len(self.regions):
            raise ValueError('Duplicate series region')
        if len(set(self.warnings))!=len(self.warnings):
            raise ValueError('Duplicate series warning')
        return self


class PackedSeriesStats(Contract):
    source:ArtifactRef
    value:SeriesStats

class SeriesStatsPack(Contract):
    stats:Annotated[list[PackedSeriesStats],Field(min_length=1,max_length=80)]

    @model_validator(mode='after')
    def unique(self):
        if len({s.value.item_id for s in self.stats})!=len(self.stats) or len({(s.source.artifact_id,s.source.sha256) for s in self.stats})!=len(self.stats):raise ValueError('Duplicate series stats pack')
        return self

class SeriesPackParams(Contract):
    stats:Annotated[list[ArtifactRef],Field(min_length=1,max_length=80)]

class SeriesParams(Contract):
    scene_id:Annotated[str,Field(min_length=1,max_length=120)]
    stats:Annotated[list[ArtifactRef],Field(max_length=5000)]=Field(default_factory=list,exclude_if=lambda value:not value)
    stats_batches:Annotated[list[ArtifactRef],Field(max_length=64)]=Field(default_factory=list,exclude_if=lambda value:not value)
    reference_item_id:UUID|None=None
    ev_limit:Literal[.35]=.35
    gain_min:Literal[.95]=.95
    gain_max:Literal[1.05]=1.05
    min_confidence:Literal[.75]=.75
    algorithm:Literal['seriesmatch_v1']='seriesmatch_v1'


    @model_validator(mode='after')
    def source(self):
        if bool(self.stats)==bool(self.stats_batches) or self.stats and len(self.stats)<2:raise ValueError('Choose stats or immutable stats packs')
        return self

class ItemCorrection(Contract):
    item_id:UUID
    raw_ev:Annotated[float,Field(ge=-10,le=10)]
    raw_gains:Annotated[list[Annotated[float,Field(ge=.01,le=100)]],Field(min_length=3,max_length=3)]
    applied_ev:Annotated[float,Field(ge=-.35,le=.35)]
    applied_gains:Annotated[list[Annotated[float,Field(ge=.95,le=1.05)]],Field(min_length=3,max_length=3)]
    confidence:Unit
    accepted:bool
    reason:Literal['applied','reference','low_confidence','no_common_regions','residual_not_improved','clipping_regression']

    @model_validator(mode='after')
    def identity_skip(self):
        if not self.accepted and (self.applied_ev!=0 or self.applied_gains!=[1.,1.,1.]):raise ValueError('Rejected series correction must be identity')
        return self


class SeriesCorrections(Contract):
    scene_id:str
    reference_item_id:UUID
    common_region_ids:Annotated[list[str],Field(max_length=32)]
    items:Annotated[list[ItemCorrection],Field(min_length=2,max_length=5000)]
    algorithm:Literal['seriesmatch_v1']='seriesmatch_v1'
    warnings:Annotated[list[Literal['no_common_regions','items_skipped']],Field(max_length=2)]

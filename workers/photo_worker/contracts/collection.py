from datetime import datetime
from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,Unit


class SceneFeatures(Contract):
    item_id:UUID
    image:ArtifactRef
    content_sha256:SHA256
    phash64:Annotated[str,Field(pattern=r'^[0-9a-f]{16}$')]
    dino:Annotated[list[float],Field(min_length=384,max_length=384)]
    stats20:Annotated[list[float],Field(min_length=20,max_length=20)]
    captured_at:datetime|None=None
    clock_offset_seconds:Annotated[int,Field(ge=-86400,le=86400)]=0
    sharpness:Annotated[float,Field(ge=0)]
    blur:Unit
    clipping:Unit
    eyes:Literal['open','closed','unknown']

    @model_validator(mode='after')
    def dino_norm(self):
        norm=sum(v*v for v in self.dino)**.5
        if not .99<=norm<=1.01:raise ValueError('DINO feature must be normalized')
        return self


class CollectionFeatureParams(Contract):
    item_id:UUID
    image:ArtifactRef
    embedding:ArtifactRef
    stats:ArtifactRef
    captured_at:datetime|None=None
    clock_offset_seconds:Annotated[int,Field(ge=-86400,le=86400)]=0

class PackedSceneFeature(Contract):
    feature:ArtifactRef
    value:SceneFeatures

class SceneFeaturePack(Contract):
    features:Annotated[list[PackedSceneFeature],Field(min_length=1,max_length=80)]

    @model_validator(mode='after')
    def unique(self):
        if len({f.value.item_id for f in self.features})!=len(self.features) or len({(f.feature.artifact_id,f.feature.sha256) for f in self.features})!=len(self.features):raise ValueError('Duplicate packed feature')
        return self

class FeaturePackParams(Contract):
    features:Annotated[list[ArtifactRef],Field(min_length=1,max_length=80)]


class ManualGroup(Contract):
    group_id:Annotated[str,Field(pattern=r'^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,119}$')]
    item_ids:Annotated[list[UUID],Field(min_length=2,max_length=5000)]


class SeparatePair(Contract):
    left:UUID
    right:UUID
    @model_validator(mode='after')
    def distinct(self):
        if self.left==self.right:raise ValueError('Self separation')
        return self


class GroupParams(Contract):
    features:Annotated[list[ArtifactRef],Field(max_length=5000)]=Field(default_factory=list,exclude_if=lambda value:not value)
    feature_batches:Annotated[list[ArtifactRef],Field(max_length=64)]=Field(default_factory=list,exclude_if=lambda value:not value)
    manual_groups:Annotated[list[ManualGroup],Field(max_length=1000)]=[]
    forced_separate:Annotated[list[SeparatePair],Field(max_length=10000)]=[]
    phash_distance:Literal[6]=6
    dino_cosine:Literal[.94]=.94
    burst_seconds:Literal[3]=3
    scene_distance:Literal[.25]=.25
    algorithm:Literal['scene_group_v1']='scene_group_v1'

    @model_validator(mode='after')
    def feature_source(self):
        if bool(self.features)==bool(self.feature_batches):raise ValueError('Choose features or immutable feature batches')
        return self


class GroupedItem(Contract):
    item_id:UUID
    feature:ArtifactRef
    exact_group_id:Annotated[str,Field(min_length=1,max_length=80)]
    burst_group_id:Annotated[str,Field(min_length=1,max_length=80)]
    scene_id:Annotated[str,Field(min_length=1,max_length=80)]


class SceneGroups(Contract):
    items:Annotated[list[GroupedItem],Field(min_length=1,max_length=5000)]
    exact_group_count:Annotated[int,Field(ge=1,le=5000)]
    burst_group_count:Annotated[int,Field(ge=1,le=5000)]
    scene_count:Annotated[int,Field(ge=1,le=5000)]
    algorithm:Literal['scene_group_v1']='scene_group_v1'
    warnings:Annotated[list[Literal['missing_capture_time','manual_override']],Field(max_length=2)]


class CullParams(Contract):
    groups:ArtifactRef
    features:Annotated[list[ArtifactRef],Field(max_length=5000)]=Field(default_factory=list,exclude_if=lambda value:not value)
    feature_batches:Annotated[list[ArtifactRef],Field(max_length=64)]=Field(default_factory=list,exclude_if=lambda value:not value)
    target_count:Annotated[int,Field(ge=1,le=5000)]|None=None
    target_fraction:Annotated[float,Field(gt=0,le=1)]|None=None
    manual_picks:Annotated[list[UUID],Field(max_length=5000)]=[]
    intentional_closed:Annotated[list[UUID],Field(max_length=5000)]=[]
    eye_policy:Literal['review']='review'
    never_delete:Literal[True]=True
    algorithm:Literal['cullrank_v1']='cullrank_v1'

    @model_validator(mode='after')
    def target(self):
        if (self.target_count is None)==(self.target_fraction is None):raise ValueError('Choose one cull target')
        if bool(self.features)==bool(self.feature_batches):raise ValueError('Choose features or immutable feature batches')
        if len(self.manual_picks)!=len(set(self.manual_picks)) or len(self.intentional_closed)!=len(set(self.intentional_closed)):raise ValueError('Duplicate cull override')
        return self


class CullItem(Contract):
    item_id:UUID
    scene_id:str
    score:Unit
    rank:Annotated[int,Field(ge=1,le=5000)]
    selected:bool
    manual:bool
    eye_review:bool


class CullRecommendations(Contract):
    groups:ArtifactRef
    target_count:Annotated[int,Field(ge=1,le=5000)]
    selected_count:Annotated[int,Field(ge=1,le=5000)]
    items:Annotated[list[CullItem],Field(min_length=1,max_length=5000)]
    never_delete:Literal[True]=True
    warnings:Annotated[list[Literal['target_below_manual_picks','selection_shortage','unknown_eye_state','closed_eye_review']],Field(max_length=4)]

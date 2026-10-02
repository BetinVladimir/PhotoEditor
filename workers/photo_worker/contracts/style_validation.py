from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from .base import ArtifactRef,Contract,SHA256,UInt32
from .images import MaskRef
from .style import MetricBlock,Quality


class ValidationPair(Contract):
    pair_id:UUID
    session_id:Annotated[str,Field(min_length=1,max_length=120)]
    before:ArtifactRef
    after:ArtifactRef
    alignment:ArtifactRef
    person:MaskRef|None=None
    ignore:MaskRef|None=None

    @model_validator(mode='after')
    def selectors(self):
        if self.person is not None and self.person.selector!='person':raise ValueError('Validation person selector mismatch')
        if self.ignore is not None and self.ignore.selector!='ignore':raise ValueError('Validation ignore selector mismatch')
        return self


class StyleValidateParams(Contract):
    style:ArtifactRef
    holdout:Annotated[list[ValidationPair],Field(min_length=1,max_length=64)]
    training_session_ids:Annotated[list[Annotated[str,Field(min_length=1,max_length=120)]],Field(min_length=1,max_length=64)]
    seed:UInt32=42
    samples_per_pair:Annotated[int,Field(ge=1024,le=65536)]=65536
    threshold_profile_id:Literal['style_validation_v1']='style_validation_v1'

    @model_validator(mode='after')
    def independent(self):
        pair_ids=[p.pair_id for p in self.holdout]
        if len(pair_ids)!=len(set(pair_ids)):raise ValueError('Duplicate validation pair')
        if set(self.training_session_ids)&{p.session_id for p in self.holdout}:raise ValueError('Validation sessions overlap training')
        return self


class StyleQualityReport(Contract):
    style:ArtifactRef
    config_hash:SHA256
    threshold_profile_id:Literal['style_validation_v1']
    pair_count:Annotated[int,Field(ge=1,le=64)]
    session_count:Annotated[int,Field(ge=1,le=64)]
    baseline:MetricBlock
    candidate:MetricBlock
    median_improvement_fraction:Annotated[float,Field(ge=-1000,le=1)]
    accepted_by_metrics:bool
    resulting_quality:Quality
    reasons:Annotated[list[Literal['insufficient_pairs','insufficient_sessions','median_improvement_below_10_percent','p95_regression','local_mask_unavailable']],Field(max_length=5)]

    @model_validator(mode='after')
    def result_consistent(self):
        if self.accepted_by_metrics != (not self.reasons):raise ValueError('Validation decision mismatch')
        if self.resulting_quality.validation!=self.candidate:raise ValueError('Validation metric mismatch')
        return self


class PackageParams(Contract):
    style:ArtifactRef
    quality_report:ArtifactRef
    package_version:Literal['style_package_v1']='style_package_v1'


class StylePackage(Contract):
    style:ArtifactRef
    quality_report:ArtifactRef
    config_hash:SHA256
    package_version:Literal['style_package_v1']='style_package_v1'
    schema_version:Literal['2.0.0']='2.0.0'
    renderer_version:Literal['photo_renderer_v2']='photo_renderer_v2'
    media_type:Literal['application/json']='application/json'
    bytes:Annotated[int,Field(ge=2,le=104857600)]

"""Strict V29 multi-input Params matching the normative schema catalogue."""
from typing import Annotated,Literal
from pydantic import Field,model_validator
from .base import Contract,ArtifactRef
from photo_worker.operations.session.contracts import VersionBinding


class MultiParams(Contract):
    schema_version:Literal[1]
    reference_ordinal:Annotated[int,Field(ge=0,le=29)]
    max_pixels:Annotated[int,Field(ge=1,le=200000000)]
    calibration:ArtifactRef

    @model_validator(mode='after')
    def scope(self):
        if self.reference_ordinal>=len(self.frames):raise ValueError('MULTI_REFERENCE_INVALID')
        versions=set();roots=set()
        for binding in self.frames:
            if binding.asset_version_id.int==0 or binding.source.artifact_id.int==0:
                raise ValueError('MULTI_SOURCE_INVALID')
            if binding.asset_version_id in versions:raise ValueError('MULTI_SOURCE_DUPLICATE')
            versions.add(binding.asset_version_id);roots.add(binding.source.artifact_id)
        if self.calibration.artifact_id.int==0 or self.calibration.artifact_id in roots:
            raise ValueError('MULTI_CALIBRATION_INVALID')
        return self


class OpticalPanoramaParams(MultiParams):
    frames:Annotated[list[VersionBinding],Field(min_length=2,max_length=20)]
    reference_ordinal:Annotated[int,Field(ge=0,le=19)]
    projection:Literal['cylindrical','planar']


class OpticalFocusStackParams(MultiParams):
    frames:Annotated[list[VersionBinding],Field(min_length=2,max_length=30)]
    motion_policy:Literal['review','reject']

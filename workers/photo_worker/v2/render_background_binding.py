"""Frozen imported background identity and explicit normalization settings."""
from typing import Annotated,Literal
from uuid import UUID
from pydantic import Field,model_validator
from photo_worker.contracts.base import ArtifactRef,Contract,SHA256,canonical_bytes,parse_json
from photo_worker.contracts.assets import DecodeParams


class FrozenBackgroundAsset(Contract):
    schema_version: Literal[1]
    portable_asset_id: str
    style_version_id: UUID
    style: ArtifactRef
    source: ArtifactRef
    expected_bytes: Annotated[int,Field(strict=True,ge=1,le=536870912)]
    media_type: Literal['image/jpeg','image/png','image/tiff']
    suffix: Literal['.jpg','.png','.tiff']
    assume_profile: Literal['srgb_v1','linear_srgb_v1','work_rec2020_gamma24_v1']|None
    work_icc_sha: SHA256

    @model_validator(mode='after')
    def bindings(self):
        if (not self.portable_asset_id or self.portable_asset_id.strip()!=self.portable_asset_id
                or len(self.portable_asset_id.encode('utf-8'))>120
                or self.style_version_id.int==0 or self.style.artifact_id.int==0
                or self.source.artifact_id.int==0
                or self.suffix!={'image/jpeg':'.jpg','image/png':'.png','image/tiff':'.tiff'}[self.media_type]):
            raise ValueError('FROZEN_BACKGROUND_BINDING_INVALID')
        return self

    def decode_params(self,*,tile=512):
        return DecodeParams(source=self.source,expected_bytes=self.expected_bytes,
            suffix=self.suffix,assume_profile=self.assume_profile,tile=tile)


def decode_frozen_background(raw):
    value=parse_json(raw)
    if len(raw)>16384 or not isinstance(value,dict) or type(value.get('schema_version')) is not int or canonical_bytes(value)!=raw:
        raise ValueError('FROZEN_BACKGROUND_NONCANONICAL')
    result=FrozenBackgroundAsset.model_validate_json(raw)
    if canonical_bytes(result)!=raw:
        raise ValueError('FROZEN_BACKGROUND_FIELDS_CHANGED')
    return result

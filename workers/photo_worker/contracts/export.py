from __future__ import annotations

from typing import Annotated, Literal

from pydantic import Field, model_validator

from .base import ArtifactRef, Contract, SHA256, Unit


class ExportSize(Contract):
    long_edge: Annotated[int, Field(ge=1, le=100000)] | None = None
    width: Annotated[int, Field(ge=1, le=100000)] | None = None
    height: Annotated[int, Field(ge=1, le=100000)] | None = None
    fit: Literal['contain'] = 'contain'
    allow_upscale: bool = False

    @model_validator(mode='after')
    def one_mode(self):
        if (self.long_edge is not None) == (self.width is not None or self.height is not None):
            raise ValueError('Specify long_edge or width+height')
        if (self.width is None) != (self.height is None):
            raise ValueError('Width and height must be paired')
        return self


class EncodeParams(Contract):
    image: ArtifactRef
    format: Literal['jpeg', 'png', 'tiff']
    icc_id: Literal['srgb_v1', 'work_rec2020_gamma24_v1']
    icc_sha256: SHA256
    size: ExportSize | None = None
    quality: Annotated[int, Field(ge=1, le=100)] | None = None
    subsampling: Literal['444'] = '444'
    alpha: Literal['preserve', 'matte']
    matte_rgb: Annotated[list[Unit], Field(min_length=3, max_length=3)] | None = None
    # export.encode always starts from pixel arrays and writes no source metadata;
    # metadata.write is the only operation allowed to apply a non-strip policy.
    metadata_policy: Literal['strip']

    @model_validator(mode='after')
    def format_options(self):
        if self.format == 'jpeg' and (self.quality is None or self.alpha != 'matte'):
            raise ValueError('JPEG requires quality and matte')
        if self.format != 'jpeg' and self.quality is not None:
            raise ValueError('Quality is JPEG-only')
        if (self.alpha == 'matte') != (self.matte_rgb is not None):
            raise ValueError('Matte color mismatch')
        return self


class EncodedImage(Contract):
    artifact: ArtifactRef
    format: Literal['jpeg', 'png', 'tiff']
    width: Annotated[int, Field(ge=1)]
    height: Annotated[int, Field(ge=1)]
    icc_id: Literal['srgb_v1', 'work_rec2020_gamma24_v1']
    icc_sha256: SHA256
    alpha: bool
    metadata_policy: Literal['strip', 'copyright_only', 'safe_camera']

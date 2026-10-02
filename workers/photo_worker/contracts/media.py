from __future__ import annotations

from typing import Annotated, Literal
from pydantic import Field, model_validator

from .base import ArtifactRef, Contract, SHA256


Variant = Literal['thumb320', 'thumb640', 'preview1600', 'preview2560']


class MediaParams(Contract):
    image: ArtifactRef
    variant: Variant
    profile_hash: SHA256
    source_kind: Literal['working', 'master'] = 'working'
    suffix: Literal['.jpg', '.jpeg', '.png', '.tif', '.tiff', '.webp'] | None = None
    matte_rgb: tuple[
        Annotated[float, Field(ge=0, le=1)],
        Annotated[float, Field(ge=0, le=1)],
        Annotated[float, Field(ge=0, le=1)],
    ] | None = None

    @model_validator(mode='after')
    def operation_variant(self):
        if (self.source_kind == 'master') != (self.suffix is not None):
            raise ValueError('Encoded master requires an explicit safe suffix')
        return self


class DerivativeManifest(Contract):
    source: ArtifactRef
    derivative: ArtifactRef
    variant: Variant
    profile_hash: SHA256
    width: Annotated[int, Field(ge=1, le=200000)]
    height: Annotated[int, Field(ge=1, le=200000)]
    media_type: Literal['image/jpeg', 'image/webp']
    color_profile: Literal['srgb']
    metadata_policy: Literal['stripped']
    alpha: bool

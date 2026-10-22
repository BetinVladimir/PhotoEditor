"""Bound region derivative metadata and encoded-byte evidence for adoption."""
from hashlib import sha256
from io import BytesIO
from typing import Annotated, Literal
from uuid import UUID

from PIL import Image
from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_hash
from .contracts import RegionGeometry, RegionPreviewParams, RegionRect,RegionProjection
from .presentation import RegionPresentation, region_cache_key


class RegionDerivativeManifest(Contract):
    schema_version: Literal[1]
    projection: RegionProjection | None = Field(default=None,exclude_if=lambda value:value is None)
    params_sha256: SHA256
    asset_version_id: UUID
    source: ArtifactRef
    recipe: ArtifactRef
    rect: RegionRect
    geometry: RegionGeometry
    derivative: ArtifactRef
    bytes: Annotated[int, Field(ge=2, le=16_777_216)]
    icc_sha256: SHA256
    cache_key: SHA256
    media_type: Literal['image/jpeg', 'image/webp']
    alpha: bool
    metadata_policy: Literal['stripped']
    quality: Literal[88]

    @model_validator(mode='after')
    def layout(self):
        if (any(value.int == 0 for value in (self.asset_version_id,
                self.source.artifact_id, self.recipe.artifact_id, self.derivative.artifact_id))
                or self.media_type != ('image/webp' if self.alpha else 'image/jpeg')
                or self.derivative.artifact_id in (self.source.artifact_id, self.recipe.artifact_id)):
            raise ValueError('REGION_RESULT_INVALID')
        return self

    def validate_for(self, params: RegionPreviewParams, target_sha256: str,
                     expected_alpha: bool) -> None:
        if (params.execution is None)!=(self.projection is None):
            raise ValueError('REGION_RESULT_PROJECTION_REQUIRED')
        if self.projection is not None:
            if (params.execution is None or self.projection.execution_sha256!=canonical_hash(params.execution)
                    or self.projection.target_canvas!=params.execution.stages[-1].target_canvas):
                raise ValueError('REGION_RESULT_PROJECTION_INVALID')
        if (self.params_sha256 != canonical_hash(params)
                or self.asset_version_id != params.request.version_id
                or self.source != params.source.ref or self.recipe != params.recipe.ref
                or self.rect != params.request.rect or self.geometry != params.geometry
                or self.icc_sha256 != target_sha256 or self.alpha != expected_alpha
                or self.cache_key != region_cache_key(params, target_sha256, expected_alpha,projection=self.projection)):
            raise ValueError('REGION_RESULT_BINDING_INVALID')


def build_region_manifest(params: RegionPreviewParams, presentation: RegionPresentation,
                          derivative: ArtifactRef, size: int) -> RegionDerivativeManifest:
    manifest = RegionDerivativeManifest(schema_version=1,
        params_sha256=canonical_hash(params), asset_version_id=params.request.version_id,projection=presentation.projection,
        source=params.source.ref, recipe=params.recipe.ref, rect=params.request.rect,
        geometry=params.geometry, derivative=derivative, bytes=size,
        icc_sha256=presentation.profile_sha256, cache_key=presentation.cache_key,
        media_type=presentation.media_type, alpha=presentation.alpha is not None,
        metadata_policy='stripped', quality=88)
    manifest.validate_for(params, presentation.profile_sha256, presentation.alpha is not None)
    return manifest


def verify_region_encoded(manifest: RegionDerivativeManifest,
                          params: RegionPreviewParams, body: bytes, *,
                          target_sha256: str, expected_alpha: bool) -> None:
    """Verify bounded bytes after catalog/version proofs; cannot prove render quality."""
    manifest.validate_for(params, target_sha256, expected_alpha)
    if len(body) != manifest.bytes or sha256(body).hexdigest() != manifest.derivative.sha256:
        raise ValueError('REGION_RESULT_BYTES_INVALID')
    geometry=params.geometry if manifest.projection is None else manifest.projection.geometry
    with Image.open(BytesIO(body)) as image:
        if (image.format != ('WEBP' if expected_alpha else 'JPEG')
                or image.size != (geometry.output_width, geometry.output_height)
                or image.mode != ('RGBA' if expected_alpha else 'RGB')
                or sha256(image.info.get('icc_profile', b'')).hexdigest() != target_sha256
                or image.info.get('exif') or image.info.get('xmp')):
            raise ValueError('REGION_RESULT_ENCODING_INVALID')
        image.verify()

"""Independent worker validation of the frozen Go region preparation plan."""
from __future__ import annotations

import math
from hashlib import sha256
from typing import Annotated, Literal
from uuid import UUID

from pydantic import Field, model_validator

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, Unit, canonical_hash, canonical_bytes, parse_json
from photo_worker.contracts.images import WorkingImage,Canvas,TransformChain
from photo_worker.operations.learning.contracts import FrozenRecipe
from .execution import RegionExecutionDescriptor


class RegionRect(Contract):
    x0: Unit
    y0: Unit
    x1: Unit
    y1: Unit

    @model_validator(mode='after')
    def ordered(self):
        if self.x0 >= self.x1 or self.y0 >= self.y1:
            raise ValueError('REGION_PREVIEW_INVALID')
        return self


class RegionRequest(Contract):
    version_id: UUID
    rect: RegionRect
    recipe: ArtifactRef
    max_edge: Annotated[int, Field(ge=64, le=1024)]


class RegionGeometry(Contract):
    left: Annotated[int, Field(ge=0)]
    top: Annotated[int, Field(ge=0)]
    width: Annotated[int, Field(ge=1)]
    height: Annotated[int, Field(ge=1)]
    output_width: Annotated[int, Field(ge=1, le=1024)]
    output_height: Annotated[int, Field(ge=1, le=1024)]


class RegionObjectPin(Contract):
    ref: ArtifactRef
    bucket: Annotated[str, Field(min_length=1, max_length=255)]
    object_key: Annotated[str, Field(min_length=1, max_length=1024)]
    object_version: Annotated[str, Field(min_length=1, max_length=1024)]
    bytes: Annotated[int, Field(ge=2, le=1_048_576)]

    @model_validator(mode='after')
    def pinned(self):
        if self.object_version == 'null' or self.ref.artifact_id.int == 0:
            raise ValueError('REGION_PREVIEW_INVALID')
        return self


class RegionInput(Contract):
    ref: ArtifactRef
    contract: Literal['WorkingImage', 'FrozenRecipe', 'StyleV2', 'NpyArray', 'EncodedImage', 'Params', 'CompositionPlan', 'ImageStructure', 'BodyWarpReport', 'PoseSet', 'FaceBoxes', 'FaceLandmarks', 'BodyFit', 'ResolvedStyle', 'Stats20', 'MaskRef','SeriesCorrections','DefectCandidates','DefectProposeReport','DefectReview','DustCandidates','DefectLabelPNG']


class RegionPreviewParams(Contract):
    schema_version: Literal[1]
    execution: RegionExecutionDescriptor | None = Field(default=None,exclude_if=lambda value:value is None)
    tenant_id: UUID
    project_id: UUID
    asset_id: UUID
    request: RegionRequest
    source: RegionObjectPin
    recipe: RegionObjectPin
    frozen_recipe: FrozenRecipe
    geometry: RegionGeometry
    render_order: Literal['full_canvas_then_crop_then_downscale']
    inputs: Annotated[list[RegionInput], Field(min_length=4, max_length=511)]

    @model_validator(mode='after')
    def binding(self):
        recipe = self.frozen_recipe
        if (any(value.int == 0 for value in (self.tenant_id, self.project_id,
                self.asset_id, self.request.version_id, recipe.base_style.artifact_id))
                or self.request.recipe != self.recipe.ref
                or recipe.source.source != self.source.ref
                or recipe.source.asset_version_id != self.request.version_id
                or canonical_hash(recipe) != self.recipe.ref.sha256
                or self.source.bucket != self.recipe.bucket):
            raise ValueError('REGION_PREVIEW_INVALID')
        if self.execution is not None and (self.execution.source!=self.source.ref
                or self.execution.source_canvas_hash!=recipe.source.canvas_hash
                or self.execution.style!=recipe.base_style):
            raise ValueError('REGION_EXECUTION_BINDING_INVALID')
        ids = [entry.ref.artifact_id for entry in self.inputs]
        if (any(value.int == 0 for value in ids) or ids != sorted(set(ids))):
            raise ValueError('REGION_INPUTS_INVALID')
        required = [(self.source.ref, 'WorkingImage'),
                    (self.recipe.ref, 'FrozenRecipe'), (recipe.base_style, 'StyleV2')]
        for ref, contract in required:
            if not any(entry.ref == ref and entry.contract == contract for entry in self.inputs):
                raise ValueError('REGION_INPUTS_INVALID')
        return self

    def validate_source(self, source_body: bytes) -> WorkingImage:
        """Check pinned source bytes before binary IO; preserve default semantics."""
        document = parse_json(source_body)
        if canonical_bytes(document) != source_body:
            raise ValueError('REGION_SOURCE_INVALID')
        image = WorkingImage.model_validate_json(source_body)
        if (sha256(source_body).hexdigest() != self.source.ref.sha256
                or canonical_hash(image.canvas) != self.frozen_recipe.source.canvas_hash):
            raise ValueError('REGION_SOURCE_INVALID')
        width, height = image.canvas.width, image.canvas.height
        if width * height > 200_000_000:
            raise ValueError('REGION_SOURCE_INVALID')
        rect = self.request.rect
        left, top = math.floor(rect.x0 * width), math.floor(rect.y0 * height)
        crop_width = math.ceil(rect.x1 * width) - left
        crop_height = math.ceil(rect.y1 * height) - top
        scale = min(1., self.request.max_edge / max(crop_width, crop_height))
        expected = RegionGeometry(left=left, top=top, width=crop_width, height=crop_height,
            output_width=max(1, math.floor(crop_width * scale)),
            output_height=max(1, math.floor(crop_height * scale)))
        if self.geometry != expected:
            raise ValueError('REGION_GEOMETRY_INVALID')
        refs = [image.pixels]
        if image.alpha is not None:
            refs.append(image.alpha)
        for step in image.transform_chain.steps:
            field = getattr(step, "inverse_field", None)
            if field is not None:
                refs.append(field)
        for ref in refs:
            if not any(entry.ref == ref and entry.contract == 'NpyArray' for entry in self.inputs):
                raise ValueError('REGION_INPUTS_INVALID')

        return image


class RegionProjection(Contract):
    """Executed geometry receipt; adoption must independently recompute it."""
    execution_sha256: SHA256
    target_canvas: Canvas
    target_transform_chain: TransformChain
    geometry: RegionGeometry

    @model_validator(mode='after')
    def target(self):
        self.target_transform_chain.validate_target(self.target_canvas)
        if (self.geometry.left+self.geometry.width>self.target_canvas.width
                or self.geometry.top+self.geometry.height>self.target_canvas.height):
            raise ValueError('REGION_PROJECTION_GEOMETRY_INVALID')
        return self

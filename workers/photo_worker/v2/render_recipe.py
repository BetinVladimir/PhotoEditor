"""Independent full authored render freeze validation; no renderer fallback."""
from __future__ import annotations
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes, canonical_hash, parse_json
from photo_worker.operations.session.contracts import VersionBinding, CorrectionVector
from photo_worker.operations.learning.contracts import FrozenRecipe


class EditingRecipeV3(Contract):
    schema_version: Literal[3]
    source: VersionBinding
    base_style: ArtifactRef
    session_corrections: ArtifactRef | None
    learning_version_id: UUID | None
    overrides: Annotated[list[ArtifactRef], Field(max_length=128)]
    layer_stack: ArtifactRef | None
    protected_mask: ArtifactRef | None = Field(default=None, exclude_if=lambda v: v is None)
    renderer_version: Literal['v2.2']
    parameter_delta: CorrectionVector

    @model_validator(mode='after')
    def identities(self):
        refs = [self.source.source, self.base_style, *self.overrides]
        refs += [v for v in (self.session_corrections, self.layer_stack, self.protected_mask) if v is not None]
        if (self.source.asset_version_id.int == 0 or any(v.artifact_id.int == 0 for v in refs)
                or self.learning_version_id is not None and self.learning_version_id.int == 0):
            raise ValueError('FROZEN_RENDER_ZERO_ID')
        return self


class FrozenRenderLearning(Contract):
    version_id: UUID
    model: ArtifactRef
    descriptor: ArtifactRef
    analysis_id: UUID
    ordinal: Annotated[int, Field(ge=0, le=4999)]


class FrozenRenderRecipe(Contract):
    schema_version: Literal[1]
    recipe: EditingRecipeV3
    recipe_sha256: SHA256
    scalars: FrozenRecipe
    learning: FrozenRenderLearning | None

    @model_validator(mode='after')
    def bindings(self):
        if (canonical_hash(self.recipe) != self.recipe_sha256
                or self.recipe.source != self.scalars.source
                or self.recipe.base_style != self.scalars.base_style
                or (self.recipe.learning_version_id is None) != (self.learning is None)):
            raise ValueError('FROZEN_RENDER_BINDING_INVALID')
        if self.learning is not None:
            if (self.learning.version_id != self.recipe.learning_version_id
                    or any(v.int == 0 for v in (self.learning.version_id, self.learning.analysis_id,
                                              self.learning.model.artifact_id, self.learning.descriptor.artifact_id))):
                raise ValueError('FROZEN_RENDER_LEARNING_INVALID')
        return self


def decode_frozen_render_recipe(raw: bytes) -> FrozenRenderRecipe:
    value = parse_json(raw)
    if canonical_bytes(value) != raw:
        raise ValueError('FROZEN_RENDER_NONCANONICAL')
    result = FrozenRenderRecipe.model_validate_json(raw)
    if canonical_bytes(result) != raw:
        raise ValueError('FROZEN_RENDER_FIELDS_CHANGED')
    return result

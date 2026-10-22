"""Original-layer composition precedes all requested Style modules."""
from typing import Literal
from uuid import UUID
from pydantic import model_validator
from .base import ArtifactRef, Contract
from photo_worker.operations.session.contracts import VersionBinding

class EditorLayersPrecomposeParams(Contract):
    schema_version: Literal[1]
    build_run_id: UUID
    original_recipe: ArtifactRef
    source: VersionBinding
    source_result: ArtifactRef | None = None

    @model_validator(mode='after')
    def identities(self):
        if (self.build_run_id.int == 0 or self.source.asset_version_id.int == 0
                or (self.source_result is not None and self.source_result.artifact_id.int == 0)):
            raise ValueError('EDITOR_PRECOMPOSE_ZERO_ID')
        return self

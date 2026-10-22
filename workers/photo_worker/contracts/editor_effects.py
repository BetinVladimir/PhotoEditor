"""Sealed executed-base origin; rebasing never waives the original recipe."""
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from .base import ArtifactRef, Contract, SHA256, canonical_hash
from .images import MaskRef
from photo_worker.operations.session.contracts import VersionBinding


class EditorCoverageStep(Contract):
    task_id: UUID
    result_id: UUID
    attempt: Annotated[int, Field(ge=1)]
    fence: Annotated[int, Field(ge=1)]
    operation: Annotated[str, Field(min_length=1,max_length=80)]
    source: ArtifactRef
    output: ArtifactRef
    report: ArtifactRef | None


class EditorEffectsSealParams(Contract):
    schema_version: Literal[1]
    build_run_id: UUID
    original_recipe: ArtifactRef
    original_source: VersionBinding
    processed_source: VersionBinding
    quality: ArtifactRef
    executed_style: ArtifactRef
    compilation_sha: SHA256
    trace_sha: SHA256
    generative_requested: bool
    generated: MaskRef | None
    inherited_review: bool
    coverage_trace: Annotated[list[EditorCoverageStep], Field(max_length=128)] = Field(default_factory=list, exclude_if=lambda v: not v)

    @model_validator(mode='after')
    def coherent(self):
        if (self.build_run_id.int == 0 or self.original_source.asset_version_id.int == 0
                or self.original_source.asset_version_id != self.processed_source.asset_version_id
                or (not self.coverage_trace and self.generative_requested != (self.generated is not None))):
            raise ValueError('EDITOR_EFFECTS_SEAL_BINDING_INVALID')
        if self.coverage_trace and canonical_hash([step.model_dump(mode="json") for step in self.coverage_trace]) != self.trace_sha:
            raise ValueError('EDITOR_EFFECTS_SEAL_TRACE_HASH')
        if self.generated is not None and (self.generated.selector != 'generated' or self.generated.protected):
            raise ValueError('EDITOR_EFFECTS_SEAL_GENERATED_INVALID')
        return self


class EditorExecutedBaseReceipt(Contract):
    schema_version: Literal[1]
    build_run_id: UUID
    original_recipe: ArtifactRef
    original_source: VersionBinding
    processed_source: VersionBinding
    executed_style: ArtifactRef
    compilation_sha: SHA256
    trace_sha: SHA256
    quality: ArtifactRef
    image_result: ArtifactRef
    generative_requested: bool
    inherited_review: bool

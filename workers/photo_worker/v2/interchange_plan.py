"""Normative interchange plan/Params with bounded, duplicate-safe decoding."""
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes, parse_json
from photo_worker.contracts.images import NormalizedRect
from photo_worker.operations.session.contracts import VersionBinding
from .interchange_archive import interchange_path


class InterchangeEntry(Contract):
    entry_id: UUID
    external_id: Annotated[str, Field(min_length=1, max_length=240)]
    path: Annotated[str, Field(min_length=1, max_length=240)]
    content: ArtifactRef
    binding: VersionBinding | None
    recipe: ArtifactRef | None
    rating: Annotated[int, Field(ge=-1, le=5)]
    pick: Literal['picked', 'rejected', 'unmarked']
    crop: NormalizedRect | None
    label: Literal['Red', 'Yellow', 'Green', 'Blue', 'Purple'] | None = Field(
        default=None, exclude_if=lambda value: value is None)

    @model_validator(mode='after')
    def identities(self):
        interchange_path(self.path)
        identities = [self.entry_id, self.content.artifact_id]
        if self.recipe is not None:
            identities.append(self.recipe.artifact_id)
        if self.binding is not None:
            identities.extend([self.binding.asset_version_id, self.binding.source.artifact_id])
        if self.binding is None and self.recipe is not None:
            raise ValueError('INTERCHANGE_RECIPE_WITHOUT_BINDING')
        if any(value.int == 0 for value in identities):
            raise ValueError('INTERCHANGE_ENTRY_IDENTITY')
        return self


class InterchangePlan(Contract):
    schema_version: Literal[1]
    project_id: UUID
    direction: Literal['import', 'export']
    format: Literal['xmp', 'photoeditor_bundle', 'captureone_sidecar']
    snapshot_digest: SHA256
    entries: Annotated[list[InterchangeEntry], Field(min_length=1, max_length=5000)]
    max_archive_bytes: Annotated[int, Field(ge=1, le=64 << 30)]
    max_expanded_bytes: Annotated[int, Field(ge=1, le=128 << 30)]
    max_files: Annotated[int, Field(ge=1, le=30000)]

    @model_validator(mode='after')
    def unique_entries(self):
        if self.project_id.int == 0:
            raise ValueError('INTERCHANGE_PLAN_PROJECT')
        if self.direction == 'export' and any(e.binding is None or e.recipe is None for e in self.entries):
            raise ValueError('INTERCHANGE_EXPORT_INCOMPLETE')
        for values in ([e.entry_id for e in self.entries], [e.external_id for e in self.entries],
                       [e.path.casefold() for e in self.entries]):
            if len(set(values)) != len(values):
                raise ValueError('INTERCHANGE_PLAN_DUPLICATE')
        return self


class InterchangeImportParams(Contract):
    schema_version: Literal[1]
    parsed: ArtifactRef
    plan: ArtifactRef
    # Omission is retained only for legacy storage-fixture decoding. Executable
    # scheduler admission requires evidence; SQL rejects four-field Params.
    evidence: ArtifactRef | None = Field(default=None, exclude_if=lambda value: value is None)
    max_report_bytes: Literal[8388608]


class InterchangeExportParams(Contract):
    schema_version: Literal[1]
    plan: ArtifactRef
    max_report_bytes: Literal[8388608]


def read_interchange_plan(raw, *, project_id, direction, snapshot_digest):
    """Bind decoded plan to scheduler evidence; never derive authority from it."""
    if (not isinstance(raw, bytes) or not isinstance(project_id, UUID) or project_id.int == 0
            or direction not in ('import', 'export')):
        raise ValueError('INTERCHANGE_PLAN_INPUT')
    value = parse_json(raw, max_bytes=8 << 20)
    stack = [(value, 1)]
    while stack:
        node, depth = stack.pop()
        if depth > 32:
            raise ValueError('INTERCHANGE_PLAN_DEPTH')
        if isinstance(node, dict):
            stack.extend((child, depth + 1) for child in node.values())
        elif isinstance(node, list):
            stack.extend((child, depth + 1) for child in node)
    plan = InterchangePlan.model_validate_json(canonical_bytes(value))
    if (plan.project_id != project_id or plan.direction != direction
            or plan.snapshot_digest != snapshot_digest):
        raise ValueError('INTERCHANGE_PLAN_SCOPE')
    return plan


def read_executable_import_params(reference, raw, *, task_id):
    from hashlib import sha256
    from uuid import uuid5
    if (not isinstance(task_id, UUID) or task_id.int == 0 or not isinstance(reference, ArtifactRef)
            or reference.artifact_id != uuid5(task_id, 'interchange:params:1')
            or not isinstance(raw, bytes) or not 2 <= len(raw) <= 65536
            or sha256(raw).hexdigest() != reference.sha256):
        raise ValueError('INTERCHANGE_EXECUTION_PARAMS_INPUT')
    value = parse_json(raw, max_bytes=65536)
    params = InterchangeImportParams.model_validate_json(canonical_bytes(value))
    if (canonical_bytes(params) != raw or params.evidence is None
            or params.plan.artifact_id != uuid5(task_id, 'interchange:plan:1')
            or params.evidence.artifact_id != uuid5(task_id, 'interchange:evidence:1')
            or len({reference.artifact_id, params.plan.artifact_id, params.evidence.artifact_id,
                    params.parsed.artifact_id}) != 4 or params.parsed.artifact_id.int == 0):
        raise ValueError('INTERCHANGE_EXECUTION_PARAMS_PIN')
    return params

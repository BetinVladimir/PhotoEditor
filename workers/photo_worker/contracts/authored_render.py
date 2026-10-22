"""Compiler DTOs for authored Render; parsing never grants task admission.

These mirror the Go CompileAuthored{Final,Segment,GeometryTask}Params checks.
Null segment layers describe a declaration; independent SQL/recipe authority
must prove a permitted no-op. Nothing here enables an operation or provider.
"""
from __future__ import annotations

import hashlib
from typing import Annotated, Literal, Self
from uuid import UUID, uuid5

from pydantic import Field, field_validator, model_validator

from .base import ArtifactRef, Contract, SHA256, canonical_bytes, parse_json

class AuthoredRef(ArtifactRef):
    @model_validator(mode='after')
    def nonzero(self) -> Self:
        if self.artifact_id.int == 0:
            raise ValueError('zero artifact identity')
        return self


class AuthoredSourceBinding(Contract):
    asset_version_id: UUID
    source: AuthoredRef
    canvas_hash: SHA256

    @model_validator(mode='after')
    def nonzero(self) -> Self:
        if self.asset_version_id.int == 0:
            raise ValueError('zero asset version')
        return self


class _Common(Contract):
    schema_version: Literal[1]
    tenant_id: UUID
    parent_run_id: UUID
    workflow_id: UUID
    instruction_id: UUID
    program_sha256: SHA256
    recipe: AuthoredRef
    base_receipt: AuthoredRef
    source: AuthoredSourceBinding

    @field_validator('schema_version', mode='before')
    @classmethod
    def exact_schema(cls, value):
        if type(value) is not int or value != 1:
            raise ValueError('invalid schema version')
        return value

    @model_validator(mode='after')
    def identities(self) -> Self:
        if any(x.int == 0 for x in (self.tenant_id, self.parent_run_id,
                                    self.workflow_id, self.instruction_id)):
            raise ValueError('zero authored identity')
        return self


class AuthoredFinalParams(_Common):
    geometry_continuation: AuthoredRef
    registered_original: AuthoredRef
    phase_receipts: Annotated[list[AuthoredRef], Field(min_length=1, max_length=4998)]

    @model_validator(mode='after')
    def phases(self) -> Self:
        ids = [r.artifact_id for r in self.phase_receipts]
        if len(set(ids)) != len(ids) or any(
            r == self.recipe or r == self.base_receipt for r in self.phase_receipts
        ):
            raise ValueError('invalid final phase receipts')
        return self


class AuthoredSegmentParams(_Common):
    ordinal: Annotated[int, Field(ge=1, le=4998)]
    predecessor_sha256: SHA256
    current_layers: AuthoredRef | None
    protected_mask: AuthoredRef | None
    phase_receipts: Annotated[list[AuthoredRef], Field(max_length=4997)]

    @model_validator(mode='after')
    def phases(self) -> Self:
        ids = [r.artifact_id for r in self.phase_receipts]
        if len(ids) != self.ordinal - 1 or len(set(ids)) != len(ids):
            raise ValueError('invalid segment phase receipts')
        return self


class AuthoredPhaseTask(Contract):
    producer_run_id: UUID
    task_id: UUID
    result_id: UUID
    operation: Literal['optical.lens', 'optical.perspective', 'optical.panorama',
                       'optical.focus_stack', 'image.upscale']
    operation_version: Literal['1']
    attempt: Annotated[int, Field(ge=1)]
    fence: Annotated[int, Field(ge=1)]
    params: AuthoredRef
    input_digest: SHA256
    execution_profile_jcs: str
    execution_profile_sha256: SHA256
    outputs: Annotated[list[AuthoredRef], Field(min_length=1, max_length=128)]

    @model_validator(mode='after')
    def producer(self) -> Self:
        if any(x.int == 0 for x in (self.producer_run_id, self.task_id, self.result_id)):
            raise ValueError('zero native identity')
        raw = self.execution_profile_jcs.encode('utf-8')
        profile = parse_json(raw, max_bytes=len(raw))
        if not isinstance(profile, dict) or not profile or canonical_bytes(profile) != raw:
            raise ValueError('noncanonical native profile')
        if hashlib.sha256(raw).hexdigest() != self.execution_profile_sha256:
            raise ValueError('native profile digest mismatch')
        ids = [r.artifact_id for r in self.outputs]
        if len(set(ids)) != len(ids):
            raise ValueError('duplicate native output')
        return self


class AuthoredGeometryPhaseWitness(Contract):
    task: AuthoredPhaseTask
    root: AuthoredRef
    image: AuthoredRef
    report: AuthoredRef


class AuthoredGeometryTaskParams(_Common):
    ordinal: Annotated[int, Field(ge=1, le=4998)]
    predecessor_sha256: SHA256
    parallel_source: AuthoredRef
    authored_params: AuthoredRef
    native: AuthoredGeometryPhaseWitness
    phase_receipts: Annotated[list[AuthoredRef], Field(max_length=4997)]

    @model_validator(mode='after')
    def geometry(self) -> Self:
        n = self.native
        if self.instruction_id != uuid5(self.workflow_id, f'render.authored_geometry:{self.ordinal}'):
            raise ValueError('geometry instruction mismatch')
        if n.task.producer_run_id in (self.parent_run_id, uuid5(self.instruction_id, 'AuthoredGeometryRun/v1')):
            raise ValueError('native producer aliases authored producer')
        if n.root not in n.task.outputs:
            raise ValueError('native root missing')
        phase_ids = [r.artifact_id for r in self.phase_receipts]
        if (len(phase_ids) != self.ordinal - 1 or len(set(phase_ids)) != len(phase_ids)
                or self.recipe.artifact_id == self.base_receipt.artifact_id
                or any(x in (self.recipe.artifact_id, self.base_receipt.artifact_id) for x in phase_ids)):
            raise ValueError('invalid geometry phases')
        refs = [self.recipe, self.base_receipt, self.source.source, self.parallel_source,
                self.authored_params, n.task.params, n.root, n.image, n.report,
                *self.phase_receipts, *n.task.outputs]
        digests: dict[UUID, str] = {}
        for r in refs:
            if r.artifact_id in digests and digests[r.artifact_id] != r.sha256:
                raise ValueError('conflicting artifact digest')
            digests[r.artifact_id] = r.sha256
        upscale = n.task.operation == 'image.upscale'
        if upscale and n.root != n.image:
            raise ValueError('upscale root is not image')
        bindings = [(self.recipe, 'FrozenRenderRecipe'), (self.base_receipt, 'AuthoredBaseReceipt'),
                    (self.source.source, 'WorkingImage'), (self.parallel_source, 'WorkingImage'),
                    (self.authored_params, 'Params'), (n.task.params, 'Params'),
                    (n.image, 'WorkingImage'), (n.report, 'UpscaleReport' if upscale else 'ImageReport'),
                    (n.root, 'WorkingImage' if upscale else 'ImageResult'),
                    *((r, 'AuthoredCanonicalPhaseReceipt') for r in self.phase_receipts)]
        roles: dict[UUID, str] = {}
        for r, role in bindings:
            if r.artifact_id in roles and roles[r.artifact_id] != role:
                raise ValueError('conflicting artifact role')
            roles[r.artifact_id] = role
        return self


def decode_authored_params(raw: bytes, kind: Literal['final', 'segment', 'geometry']):
    """Bounded duplicate-key-safe wire parser. No admission or physical claims."""
    parse_json(raw, max_bytes=32 * 1024 * 1024)
    models = {'final': AuthoredFinalParams, 'segment': AuthoredSegmentParams,
              'geometry': AuthoredGeometryTaskParams}
    if kind not in models:
        raise ValueError('unknown authored params kind')
    return models[kind].model_validate_json(raw)

"""Leased import adapter; dispatch requires installed SQL admission and adoption."""
from dataclasses import dataclass
from hashlib import sha256
from typing import Protocol
from uuid import UUID

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.interchange_frozen_import import prepare_import_from_params
from photo_worker.v2.interchange_mapping import MappingReport, MappingShard
from photo_worker.v2.interchange_plan import InterchangeImportParams


@dataclass(frozen=True)
class ImportAuthority:
    workflow_id: UUID
    project_id: UUID
    snapshot_digest: str
    max_input_json_bytes: int
    max_output_bytes: int


class ImportAdmission(Protocol):
    def admit(self, ctx, params: InterchangeImportParams) -> ImportAuthority:
        """Prove current attempt/lease, ACL, frozen evidence, closure and quota."""
        ...


@dataclass(frozen=True)
class ImportInstallation:
    admission: ImportAdmission
    recipe_qualifier_factory: object = None


def execute_import(ctx, artifacts, installation: ImportInstallation):
    ctx.check_active()
    command, profile = ctx.lease.command, ctx.lease.execution_profile
    if not isinstance(profile, dict):
        raise OperationFailure('CONFIG_INVALID')
    estimate = profile.get('resource_estimate', {})
    if (command.operation != 'v2.interchange.import' or command.operation_version != '1'
            or command.resource_class != 'cpu' or profile.get('backend') != 'cpu'
            or profile.get('renderer_version') != 'v2.2' or not isinstance(estimate, dict)
            or type(estimate.get('vram_bytes')) is not int or estimate['vram_bytes'] != 0):
        raise OperationFailure('CONFIG_INVALID')
    if (not isinstance(installation, ImportInstallation)
            or not callable(getattr(installation.admission, 'admit', None))
            or installation.recipe_qualifier_factory is not None and not callable(installation.recipe_qualifier_factory)):
        raise OperationFailure('INTERCHANGE_ADMISSION_UNAVAILABLE')
    def admit(params):
        ctx.check_active()
        candidate = params.model_copy(deep=True)
        expected = canonical_bytes(candidate)
        value = installation.admission.admit(ctx, candidate)
        ctx.check_active()
        if canonical_bytes(candidate) != expected:
            raise ValueError('import admission mutated input')
        return value

    try:
        params = artifacts.params(InterchangeImportParams).model_copy(deep=True)
        ctx.check_active()
        if params.evidence is None or sha256(canonical_bytes(params)).hexdigest() != command.params.sha256:
            raise ValueError('executable evidence Params required')
        authority = admit(params)
        if (not isinstance(authority, ImportAuthority)
                or not isinstance(authority.workflow_id, UUID) or authority.workflow_id.int == 0
                or not isinstance(authority.project_id, UUID) or authority.project_id.int == 0
                or not isinstance(authority.snapshot_digest, str) or len(authority.snapshot_digest) != 64
                or any(c not in '0123456789abcdef' for c in authority.snapshot_digest)
                or type(authority.max_input_json_bytes) is not int or not 1 <= authority.max_input_json_bytes <= 128 << 20
                or type(authority.max_output_bytes) is not int or not 1 <= authority.max_output_bytes <= 2**53-1
                or type(estimate.get('output_bytes')) is not int or authority.max_output_bytes > estimate['output_bytes']):
            raise OperationFailure('INTERCHANGE_ADMISSION_UNAVAILABLE')
        from photo_worker.v2.interchange_read_budget import ImportReadBudget
        read_budget = ImportReadBudget(authority.max_input_json_bytes)
        written = 0
        staged = []

        def current():
            ctx.check_active()
            if admit(params) != authority:
                raise OperationFailure('INTERCHANGE_SOURCE_STALE')

        def read(reference, maximum):
            ctx.check_active()
            if type(maximum) is not int or not 2 <= maximum <= 8 << 20:
                raise ValueError('document budget invalid')
            limit = min(maximum, read_budget.remaining_json_bytes)
            if limit < 2:
                raise ValueError('aggregate document budget exceeded')
            artifacts.require_declared_input(reference)
            chunks, size = [], 0
            with artifacts.stream(reference) as source:
                while True:
                    ctx.check_active()
                    block = source.read(min(65536, limit + 1 - size))
                    if not block:
                        break
                    size += len(block)
                    if size > limit:
                        raise ValueError('document budget exceeded')
                    read_budget.consume(len(block), is_json=True)
                    chunks.append(block)
            ctx.check_active()
            raw = b''.join(chunks)
            if size < 2 or sha256(raw).hexdigest() != reference.sha256:
                raise ValueError('declared document hash changed')
            return raw

        def stage(document, role):
            nonlocal written
            current()
            size = len(canonical_bytes(document))
            if written + size > authority.max_output_bytes:
                raise ValueError('output reservation exceeded')
            reference = artifacts.stage_contract(document, role)
            if reference.sha256 != sha256(canonical_bytes(document)).hexdigest() or any(ref.artifact_id == reference.artifact_id for ref in staged):
                raise ValueError('output identity collision')
            staged.append(reference)
            written += size
            current()
            return reference

        def publish(raw, contract, ordinal):
            if contract == 'MappingShard':
                model, role = MappingShard, 'mapping_shard'
            elif contract == 'MappingReport':
                model, role = MappingReport, 'report'
            else:
                raise ValueError('unexpected import output contract')
            document = model.model_validate_json(raw)
            if canonical_bytes(document) != raw:
                raise ValueError('noncanonical import output')
            return stage(document, role)

        qualifier = None
        if installation.recipe_qualifier_factory is not None:
            factory = installation.recipe_qualifier_factory
            shared_builder = getattr(factory, 'build_with_read_budget', None)
            if not callable(shared_builder):
                raise OperationFailure('INTERCHANGE_READ_BUDGET_REQUIRED')
            qualifier = shared_builder(ctx, artifacts, authority, read_budget)
            if not callable(qualifier):
                raise OperationFailure('INTERCHANGE_ADMISSION_UNAVAILABLE')
        current()
        result = prepare_import_from_params(params_ref=command.params, params_jcs=canonical_bytes(params),
            task_id=command.task_id, workflow_id=authority.workflow_id, project_id=authority.project_id,
            snapshot_digest=authority.snapshot_digest, read_document=read, publish_document=publish,
            publish_root=lambda document: stage(document, 'document'), qualify_recipe=qualifier,
            check_active=ctx.check_active)
        current()
        return OperationResult(outputs=[result.root, *[ref for ref in staged if ref != result.root]],
            review_required=bool(result.mapping.proposals), bytes_read=read_budget.input_bytes, bytes_written=written,
            peak_device_bytes=0)
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc


def interchange_import_operation(config, installation: ImportInstallation):
    if not isinstance(installation, ImportInstallation) or not callable(getattr(installation.admission, 'admit', None)):
        raise ValueError('INTERCHANGE_INSTALLATION_INVALID')
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'document', 'report', 'mapping_shard'})) as artifacts:
            return execute_import(ctx, artifacts, installation)
    return Operation('v2.interchange.import', 'cpu', run)

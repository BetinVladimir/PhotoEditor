"""Leased parse adapter. Installation requires independent SQL/source authority."""
from dataclasses import dataclass
from hashlib import sha256
from typing import Annotated, Literal, Protocol
from uuid import UUID

from pydantic import Field
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.interchange_archive import ArchiveLimits
from photo_worker.v2.interchange_parse import stage_photoeditor_bundle
from photo_worker.v2.interchange_sidecar_parse import stage_sidecar_archive


class ImportRequest(Contract):
    project_id: UUID
    direction: Literal["import"]
    format: Literal["xmp", "photoeditor_bundle", "captureone_sidecar"]
    bundle: ArtifactRef


class InterchangeParseParams(Contract):
    schema_version: Literal[1]
    request: ImportRequest
    max_expanded_bytes: Annotated[int, Field(ge=1, le=128 << 30)]
    max_files: Annotated[int, Field(ge=1, le=30000)]


class ParseAdmission(Protocol):
    def admit(self, ctx, params: InterchangeParseParams) -> ArchiveLimits:
        """Current worker fence, project/bundle scope and reserved byte budgets.

        Returned quotas must come from authoritative task/tenant reservations,
        not the submitted bundle or client. Repeat after IO before completion.
        """
        ...


@dataclass(frozen=True)
class ParseInstallation:
    admission: ParseAdmission


def execute_parse(ctx, artifacts, installation: ParseInstallation):
    ctx.check_active()
    command = ctx.lease.command
    profile = ctx.lease.execution_profile
    if (command.operation != "v2.interchange.parse" or command.operation_version != "1"
            or command.resource_class != "cpu" or not isinstance(profile, dict) or profile.get("backend") != "cpu"
            or profile.get("renderer_version") != "v2.2"):
        raise OperationFailure("CONFIG_INVALID")
    if not isinstance(installation, ParseInstallation) or not callable(getattr(installation.admission, "admit", None)):
        raise OperationFailure("INTERCHANGE_ADMISSION_UNAVAILABLE")
    def admit(params):
        ctx.check_active()
        candidate = params.model_copy(deep=True)
        expected = canonical_bytes(candidate)
        result = installation.admission.admit(ctx, candidate)
        ctx.check_active()
        if canonical_bytes(candidate) != expected:
            raise ValueError("parse admission mutated input")
        return result

    try:
        params = artifacts.params(InterchangeParseParams).model_copy(deep=True)
        ctx.check_active()
        if (params.request.project_id.int == 0 or params.request.bundle.artifact_id.int == 0
                or sha256(canonical_bytes(params)).hexdigest() != command.params.sha256):
            raise ValueError("invalid parse identity")
        artifacts.require_declared_input(params.request.bundle.model_copy(deep=True))
        ctx.check_active()
        authorized = admit(params)
        if not isinstance(authorized, ArchiveLimits):
            raise OperationFailure("INTERCHANGE_ADMISSION_UNAVAILABLE")
        authorized.validate()
        limits = ArchiveLimits(authorized.archive_bytes,
                               min(authorized.expanded_bytes, params.max_expanded_bytes),
                               min(authorized.files, params.max_files))
        if params.request.format == "photoeditor_bundle":
            staged = stage_photoeditor_bundle(artifacts, params.request.bundle.model_copy(deep=True), limits, ctx.check_active)
        else:
            staged = stage_sidecar_archive(artifacts, params.request.bundle.model_copy(deep=True), limits,
                format=params.request.format, active=ctx.check_active)
        ctx.check_active()
        current = admit(params)
        if current != authorized:
            raise OperationFailure("INTERCHANGE_SOURCE_STALE")
        root = artifacts.stage_contract(staged.document, "document")
        ctx.check_active()
        if admit(params) != authorized:
            raise OperationFailure("INTERCHANGE_SOURCE_STALE")
        return OperationResult(outputs=[root, *staged.outputs], review_required=False,
                               bytes_written=staged.bytes_written + len(canonical_bytes(staged.document)))
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc


def interchange_parse_operation(config, installation: ParseInstallation):
    if not isinstance(installation, ParseInstallation) or not callable(getattr(installation.admission, "admit", None)):
        raise ValueError("INTERCHANGE_INSTALLATION_INVALID")

    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"document", "imported_file"})) as artifacts:
            return execute_parse(ctx, artifacts, installation)

    return Operation("v2.interchange.parse", "cpu", run)

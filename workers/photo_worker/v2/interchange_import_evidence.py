"""Immutable scheduler evidence transported as a declared import input."""
from dataclasses import dataclass
from hashlib import sha256
from typing import Annotated, Literal
from uuid import UUID, uuid5

from pydantic import Field
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from .adoption import AdoptionInvalid
from .interchange_import_sources import ImportSources
from .interchange_metadata import MetadataReceipts, qualified_metadata_mapper
from .interchange_parse import ParsedInterchange, validate_parsed_inventory
from .interchange_plan import InterchangePlan


class ImportRecipeDiagnostic(Contract):
    entry_id: UUID
    reason: Annotated[str, Field(min_length=1, max_length=200)]


class FrozenInterchangeImportEvidence(Contract):
    schema_version: Literal[1]
    workflow_id: UUID
    project_id: UUID
    plan: ArtifactRef
    parsed: ArtifactRef
    sources: ImportSources
    metadata: MetadataReceipts
    recipe_diagnostics: Annotated[list[ImportRecipeDiagnostic], Field(max_length=5000)]


@dataclass(frozen=True)
class PreparedImportEvidence:
    artifact_id: UUID
    body: bytes

    @property
    def reference(self):
        return ArtifactRef(artifact_id=self.artifact_id, sha256=sha256(self.body).hexdigest())


def freeze_import_evidence(*, workflow_id, plan_ref, plan, parsed_jcs, sources,
                           metadata, recipe_diagnostics, check_active=lambda: None):
    """Compiler diagnostics are explicit; terminal ingest reasons stay in sources.

    The publisher must reserve/complete this exact body against current SQL
    sources and metadata. This compiler alone grants no publication authority.
    """
    check_active()
    if (not isinstance(workflow_id, UUID) or workflow_id.int == 0
            or not isinstance(plan_ref, ArtifactRef) or plan_ref.artifact_id.int == 0
            or not isinstance(plan, InterchangePlan) or not isinstance(sources, ImportSources)
            or not isinstance(metadata, MetadataReceipts) or not isinstance(recipe_diagnostics, dict)
            or not isinstance(parsed_jcs, bytes) or not 2 <= len(parsed_jcs) <= 8 << 20):
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_INPUT')
    plan = InterchangePlan.model_validate_json(canonical_bytes(plan))
    sources = ImportSources.model_validate_json(canonical_bytes(sources))
    metadata = MetadataReceipts.model_validate_json(canonical_bytes(metadata))
    parsed = ParsedInterchange.model_validate_json(parsed_jcs)
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_INVENTORY') from error
    task = uuid5(workflow_id, 'v2.interchange.import:0')
    if (sources.workflow_id != workflow_id or plan.project_id != sources.project_id or plan.direction != 'import'
            or plan.format != sources.format
            or plan_ref.artifact_id != uuid5(task, 'interchange:plan:1')
            or sha256(canonical_bytes(plan)).hexdigest() != plan_ref.sha256
            or canonical_bytes(parsed) != parsed_jcs or sha256(parsed_jcs).hexdigest() != sources.parsed.sha256
            or len(plan.entries) != len(parsed.manifest.entries) or len(plan.entries) != len(sources.entries)):
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_SCOPE')
    mapper = qualified_metadata_mapper(metadata, sources)
    files = {file.path: file.content for file in parsed.files}
    diagnostics = dict(recipe_diagnostics)
    ordered = []
    for ordinal, (entry, original, source) in enumerate(zip(plan.entries, parsed.manifest.entries, sources.entries)):
        check_active()
        if (source.ordinal != ordinal or source.entry_id != original.entry_id
                or entry.entry_id != original.entry_id or entry.external_id != original.external_id
                or entry.path != original.image_path or entry.content != files.get(original.image_path)
                or entry.rating != original.rating or entry.pick != original.pick or entry.crop != original.crop
                or entry.label != original.label):
            raise AdoptionInvalid('INTERCHANGE_EVIDENCE_ENTRY')
        if source.state != 'adopted':
            if entry.binding is not None or entry.recipe is not None:
                raise AdoptionInvalid('INTERCHANGE_EVIDENCE_TERMINAL')
        else:
            if (entry.binding is None or entry.binding.asset_version_id != source.asset_version_id
                    or entry.binding.source != source.master or entry.recipe is not None and original.recipe_path is None):
                raise AdoptionInvalid('INTERCHANGE_EVIDENCE_BINDING')
            mapper(original.model_copy(deep=True), entry.binding.model_copy(deep=True))
            if entry.recipe is None and original.recipe_path is not None:
                reason = diagnostics.pop(entry.entry_id, None)
                if not isinstance(reason, str) or not 1 <= len(reason) <= 200:
                    raise AdoptionInvalid('INTERCHANGE_EVIDENCE_DIAGNOSTIC')
                ordered.append(ImportRecipeDiagnostic(entry_id=entry.entry_id, reason=reason))
    if diagnostics:
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_FOREIGN_DIAGNOSTIC')
    document = FrozenInterchangeImportEvidence(schema_version=1, workflow_id=workflow_id,
        project_id=plan.project_id, plan=plan_ref.model_copy(deep=True), parsed=sources.parsed.model_copy(deep=True),
        sources=sources, metadata=metadata, recipe_diagnostics=ordered)
    body = canonical_bytes(document)
    if len(body) > 8 << 20:
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_SIZE')
    reference = ArtifactRef(artifact_id=uuid5(task, 'interchange:evidence:1'), sha256=sha256(body).hexdigest())
    if reference.artifact_id in (plan_ref.artifact_id, sources.parsed.artifact_id):
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_ALIAS')
    check_active()
    return PreparedImportEvidence(reference.artifact_id, body)


def read_import_evidence(reference, raw, *, workflow_id, plan_ref, plan, parsed_jcs,
                         check_active=lambda: None):
    check_active()
    if (not isinstance(reference, ArtifactRef) or not isinstance(raw, bytes)
            or not 2 <= len(raw) <= 8 << 20 or sha256(raw).hexdigest() != reference.sha256):
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_HASH')
    document = FrozenInterchangeImportEvidence.model_validate_json(raw)
    diagnostics = {item.entry_id: item.reason for item in document.recipe_diagnostics}
    if len(diagnostics) != len(document.recipe_diagnostics):
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_DUPLICATE_DIAGNOSTIC')
    prepared = freeze_import_evidence(workflow_id=workflow_id, plan_ref=plan_ref, plan=plan,
        parsed_jcs=parsed_jcs, sources=document.sources, metadata=document.metadata,
        recipe_diagnostics=diagnostics, check_active=check_active)
    if prepared.reference != reference or prepared.body != raw:
        raise AdoptionInvalid('INTERCHANGE_EVIDENCE_CHANGED')
    return document

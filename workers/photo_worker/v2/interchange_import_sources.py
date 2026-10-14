"""Strict current post-ingest inventory; caller owns tenant-scoped SQL context."""
from typing import Annotated, Literal
from uuid import UUID, uuid5
from pydantic import Field, ValidationError, model_serializer, model_validator
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes, parse_json
from .interchange_parse import ParsedInterchange, validate_parsed_inventory
from .adoption import AdoptionInvalid


class ImportSourceEntry(Contract):
    ordinal: Annotated[int, Field(ge=0, le=4999)]
    entry_id: UUID
    state: Literal['adopted','rejected','unsupported']
    reason: Annotated[str, Field(min_length=1, max_length=200)] | None
    asset_version_id: UUID | None
    master: ArtifactRef | None

    @model_validator(mode='after')
    def state_shape(self):
        if self.entry_id.int == 0:
            raise ValueError('invalid entry identity')
        if self.state == 'adopted':
            if (self.reason is not None or self.asset_version_id is None or self.asset_version_id.int == 0
                    or self.master is None or self.master.artifact_id.int == 0):
                raise ValueError('invalid adopted source')
        elif self.reason is None or self.asset_version_id is not None or self.master is not None:
            raise ValueError('invalid terminal source')
        return self


class ImportSources(Contract):
    schema_version: Literal[1]
    workflow_id: UUID
    project_id: UUID
    format: Literal['photoeditor_bundle', 'xmp', 'captureone_sidecar'] = 'photoeditor_bundle'
    parsed: ArtifactRef
    ingest_run_id: UUID | None
    entries: Annotated[list[ImportSourceEntry], Field(min_length=1, max_length=5000)]

    @model_validator(mode='after')
    def child_run_scope(self):
        needs_ingest = any(entry.state != 'unsupported' for entry in self.entries)
        expected = uuid5(self.workflow_id, 'interchange:canonical-ingest:1')
        if (self.workflow_id.int == 0 or self.project_id.int == 0
                or needs_ingest != (self.ingest_run_id is not None)
                or self.ingest_run_id is not None and self.ingest_run_id != expected
                or len({entry.entry_id for entry in self.entries}) != len(self.entries)
                or any(entry.ordinal != ordinal for ordinal, entry in enumerate(self.entries))):
            raise ValueError('invalid import source inventory')
        return self

    @model_serializer(mode='wrap')
    def serialize_legacy_bundle(self, handler):
        value = handler(self)
        if self.format == 'photoeditor_bundle':
            value.pop('format', None)
        return value


def load_import_sources(connection, tenant_id, workflow_id, parsed_ref, parsed_jcs, *, check_active=lambda: None):
    from hashlib import sha256
    if (any(not isinstance(value,UUID) or value.int==0 for value in (tenant_id,workflow_id))
            or not isinstance(parsed_jcs,bytes) or not 2<=len(parsed_jcs)<=8<<20
            or sha256(parsed_jcs).hexdigest()!=parsed_ref.sha256):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_SOURCE_INPUT')
    check_active()
    row=connection.execute('SELECT photo.v30_interchange_import_sources(%s,%s,%s)',
        (tenant_id,workflow_id,parsed_jcs.decode('utf-8'))).fetchone()
    check_active()
    if row is None or row[0] is None:
        return None
    value=row[0]
    if isinstance(value,str):
        value=parse_json(value,max_bytes=8<<20)
    try:
        result=ImportSources.model_validate_json(canonical_bytes(value))
    except ValidationError as error:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_SOURCE_MISMATCH') from error
    parsed=ParsedInterchange.model_validate_json(parsed_jcs)
    try:
        validate_parsed_inventory(parsed)
    except ValueError as error:
        raise AdoptionInvalid('INTERCHANGE_IMPORT_SOURCE_INVENTORY') from error
    if (result.workflow_id!=workflow_id or result.project_id.int==0 or result.parsed!=parsed_ref
            or len(result.entries)!=len(parsed.manifest.entries)
            or any(actual.ordinal!=index or actual.entry_id!=expected.entry_id
                for index,(actual,expected) in enumerate(zip(result.entries,parsed.manifest.entries)))
            or len({entry.entry_id for entry in result.entries})!=len(result.entries)
            or any(entry.state!='unsupported' for entry in result.entries) != (result.ingest_run_id is not None)
            or result.ingest_run_id is not None and result.ingest_run_id != uuid5(workflow_id, 'interchange:canonical-ingest:1')):
        raise AdoptionInvalid('INTERCHANGE_IMPORT_SOURCE_MISMATCH')
    check_active()
    return result

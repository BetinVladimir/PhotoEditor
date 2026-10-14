"""Qualify mapping claims against actual new-collection metadata receipts."""
from typing import Annotated, Literal
from uuid import UUID, uuid5
from pydantic import Field
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes
from .adoption import AdoptionInvalid
from .interchange_mapping import MappingField


class MetadataReceipt(Contract):
    entry_id: UUID
    asset_version_id: UUID
    master: ArtifactRef
    item_id: UUID
    rating: Annotated[int, Field(ge=0, le=5)]
    flag: Literal['pick','reject','none']
    label: Literal['Red','Yellow','Green','Blue','Purple'] | None = Field(
        default=None, exclude_if=lambda value: value is None)


class MetadataReceipts(Contract):
    schema_version: Literal[1]
    workflow_id: UUID
    project_id: UUID
    parsed: ArtifactRef
    collection_id: UUID
    entries: Annotated[list[MetadataReceipt], Field(max_length=5000)]


def qualified_metadata_mapper(receipts, sources):
    """Receipt must be rechecked by final SQL adoption, not treated as approval."""
    from .interchange_import_sources import ImportSources
    sources=ImportSources.model_validate_json(canonical_bytes(sources))
    receipts=MetadataReceipts.model_validate_json(canonical_bytes(receipts))
    adopted={entry.entry_id:entry for entry in sources.entries if entry.state=='adopted'}
    rows={entry.entry_id:entry for entry in receipts.entries}
    if (receipts.workflow_id!=sources.workflow_id or receipts.project_id!=sources.project_id
            or receipts.parsed!=sources.parsed
            or receipts.collection_id!=uuid5(sources.workflow_id,'interchange:draft-collection:1')
            or len(rows)!=len(receipts.entries) or set(rows)!=set(adopted)):
        raise AdoptionInvalid('INTERCHANGE_METADATA_SCOPE')
    run=uuid5(sources.workflow_id,'interchange:canonical-ingest:1')
    for identifier,row in rows.items():
        source=adopted[identifier]
        if (row.item_id!=uuid5(run,'item:'+str(source.ordinal))
                or row.asset_version_id!=source.asset_version_id or row.master!=source.master):
            raise AdoptionInvalid('INTERCHANGE_METADATA_BINDING')

    def map_metadata(entry,binding):
        row=rows.get(entry.entry_id)
        if (row is None or row.asset_version_id!=binding.asset_version_id or row.master!=binding.source
                or row.rating!=max(0,entry.rating)
                or row.flag!={'picked':'pick','rejected':'reject','unmarked':'none'}[entry.pick]
                or row.label!=getattr(entry, 'label', None)):
            raise AdoptionInvalid('INTERCHANGE_METADATA_CHANGED')
        fields = [MappingField(name='rating',status='exact' if entry.rating>=0 else 'unsupported',
                    reason=None if entry.rating>=0 else 'negative_rating_has_no_local_equivalent'),
                MappingField(name='pick',status='exact',reason=None),
                MappingField(name='crop',status='exact' if entry.crop is None else 'unsupported',
                    reason=None if entry.crop is None else 'crop_requires_qualified_recipe_transform')]
        if getattr(entry, 'label', None) is not None:
            fields.append(MappingField(name='label', status='exact', reason=None))
        return fields
    return map_metadata


def load_import_metadata(connection, tenant_id, workflow_id, parsed_ref, parsed_jcs,
                         *, check_active=lambda: None):
    """Read current sources and saved metadata in the caller's tenant transaction.

    No object-store access or transaction management occurs here. NULL means the
    import collection is not ready (or access was revoked), never an empty receipt.
    The returned receipt still needs final fenced adoption revalidation.
    """
    from .interchange_import_sources import load_import_sources
    from photo_worker.contracts.base import parse_json
    sources = load_import_sources(connection, tenant_id, workflow_id, parsed_ref,
                                  parsed_jcs, check_active=check_active)
    if sources is None:
        return None
    check_active()
    row = connection.execute(
        'SELECT photo.v30_interchange_metadata_receipts(%s,%s,%s)',
        (tenant_id, workflow_id, parsed_jcs.decode('utf-8'))).fetchone()
    check_active()
    if row is None or row[0] is None:
        return None
    value = row[0]
    if isinstance(value, str):
        value = parse_json(value, max_bytes=8 << 20)
    encoded = canonical_bytes(value)
    if len(encoded) > 8 << 20:
        raise AdoptionInvalid('INTERCHANGE_METADATA_SIZE')
    receipts = MetadataReceipts.model_validate_json(encoded)
    # Validate complete adopted-entry coverage and deterministic collection/items.
    qualified_metadata_mapper(receipts, sources)
    check_active()
    return sources, receipts

"""Bounded import mapping/report documents; no approval mutations."""
from dataclasses import dataclass
from hashlib import sha256
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, model_validator
from photo_worker.contracts.base import ArtifactRef, Contract, canonical_bytes


class MappingField(Contract):
    name: Annotated[str, Field(min_length=1, max_length=80)]
    status: Literal['exact', 'unsupported', 'rejected']
    reason: Annotated[str, Field(min_length=1, max_length=200)] | None

    @model_validator(mode='after')
    def reason_matches_status(self):
        if (self.status == 'exact') != (self.reason is None):
            raise ValueError('mapping reason does not match status')
        return self


class MappingEntry(Contract):
    entry_id: UUID
    external_id: Annotated[str, Field(min_length=1, max_length=240)]
    asset_version_id: UUID | None
    proposed_document: ArtifactRef | None
    fields: Annotated[list[MappingField], Field(max_length=128)]

    @model_validator(mode='after')
    def validate_identity(self):
        if (self.entry_id.int == 0 or self.asset_version_id is not None and self.asset_version_id.int == 0
                or self.proposed_document is not None and (self.asset_version_id is None or self.proposed_document.artifact_id.int == 0)
                or len({field.name for field in self.fields}) != len(self.fields)):
            raise ValueError('invalid mapping entry identity or fields')
        return self


class MappingShard(Contract):
    schema_version: Literal[1]
    entries: Annotated[list[MappingEntry], Field(min_length=1, max_length=100)]


class MappingReport(Contract):
    schema_version: Literal[1]
    entry_count: Annotated[int, Field(ge=0, le=5000)]
    shards: Annotated[list[ArtifactRef], Field(max_length=50)]
    rejected_count: Annotated[int, Field(ge=0, le=5000)]
    unsupported_count: Annotated[int, Field(ge=0, le=5000)]


@dataclass(frozen=True)
class PreparedMappingReport:
    report: ArtifactRef
    report_jcs: bytes
    shards: tuple[ArtifactRef, ...]
    shard_bodies: tuple[bytes, ...]
    proposals: tuple[ArtifactRef, ...]


def publish_mapping_report(entries, expected_entries, publish_document, *, check_active=lambda: None):
    """Publisher owns task scope/roles and returns gateway-issued references.

    expected_entries is the ordered (entry_id, external_id) inventory from the
    independently verified parse. Object publication finishes before adoption;
    caller must atomically recheck these exact documents and current authority.
    """
    check_active()
    source = tuple(expected_entries)
    owned = tuple(MappingEntry.model_validate_json(canonical_bytes(entry)) for entry in entries)
    if len(owned) > 5000 or len(source) != len(owned) or not callable(publish_document):
        raise ValueError('mapping inventory limit or count mismatch')
    identities = tuple((entry.entry_id, entry.external_id) for entry in owned)
    versions = [entry.asset_version_id for entry in owned if entry.asset_version_id is not None]
    proposals = tuple(entry.proposed_document for entry in owned if entry.proposed_document is not None)
    if (identities != source or len({item[0] for item in identities}) != len(owned)
            or len({item[1] for item in identities}) != len(owned)
            or len(set(versions)) != len(versions)
            or len({item.artifact_id for item in proposals}) != len(proposals)):
        raise ValueError('mapping source identity mismatch or duplicate output')
    used = {item.artifact_id for item in proposals}

    def publish(body, contract, ordinal):
        check_active()
        result = publish_document(body, contract, ordinal)
        if (not isinstance(result, ArtifactRef) or result.artifact_id.int == 0
                or result.artifact_id in used or result.sha256 != sha256(body).hexdigest()):
            raise ValueError('mapping publication mismatch')
        used.add(result.artifact_id)
        check_active()
        return result.model_copy(deep=True)

    shards, bodies = [], []
    for offset in range(0, len(owned), 100):
        body = canonical_bytes(MappingShard(schema_version=1, entries=list(owned[offset:offset+100])))
        shards.append(publish(body, 'MappingShard', len(shards)))
        bodies.append(body)
    report = MappingReport(schema_version=1, entry_count=len(owned), shards=shards,
        rejected_count=sum(any(field.status == 'rejected' for field in entry.fields) for entry in owned),
        unsupported_count=sum(any(field.status == 'unsupported' for field in entry.fields) for entry in owned))
    body = canonical_bytes(report)
    root = publish(body, 'MappingReport', 0)
    return PreparedMappingReport(root, body, tuple(shards), tuple(bodies), proposals)

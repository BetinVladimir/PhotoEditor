"""V31 closed contracts; validation does not grant signing/key authority."""
from typing import Annotated, Literal
from uuid import UUID
from pydantic import Field, field_validator, model_validator
from .base import ArtifactRef, Contract, SHA256, canonical_bytes, parse_json


def _text(value, maximum):
    if not value or len(value.encode('utf-8')) > maximum or value.strip() != value or any(c in value for c in '\x00\r\n'):
        raise ValueError('PROVENANCE_TEXT')
    return value


class VersionedProvenance(Contract):
    schema_version: Literal[1]

    @field_validator('schema_version', mode='before')
    @classmethod
    def integer_schema(cls, value):
        if type(value) is not int:
            raise ValueError('PROVENANCE_SCHEMA')
        return value


class SigningPolicy(VersionedProvenance):
    id: UUID
    revision: Annotated[str, Field(pattern=r'^[1-9][0-9]{0,18}$')]
    sdk_lock_sha: SHA256
    key_handle: str
    certificate_chain_sha: SHA256
    timestamp_mode: Literal['off', 'required']
    timestamp_service_id: str | None
    allowed_mime: Annotated[list[Literal['image/jpeg', 'image/png']], Field(min_length=1, max_length=2)]
    trust_bundle_sha: SHA256
    revocation_mode: Literal['off', 'required']

    @model_validator(mode='after')
    def installed_policy_shape(self):
        if self.id.int == 0 or int(self.revision) > 9223372036854775807:
            raise ValueError('PROVENANCE_POLICY_IDENTITY')
        _text(self.key_handle, 200)
        if len(set(self.allowed_mime)) != len(self.allowed_mime):
            raise ValueError('PROVENANCE_MIME_DUPLICATE')
        if (self.timestamp_mode == 'required') != (self.timestamp_service_id is not None):
            raise ValueError('PROVENANCE_TIMESTAMP')
        if self.timestamp_service_id is not None:
            _text(self.timestamp_service_id, 120)
        return self


class SigningAction(Contract):
    operation: str
    version: str
    params_sha: SHA256
    model_lock_sha: SHA256 | None
    generated: bool
    review_id: UUID | None

    @model_validator(mode='after')
    def claims(self):
        _text(self.operation, 80)
        _text(self.version, 20)
        if self.review_id is not None and self.review_id.int == 0:
            raise ValueError('PROVENANCE_REVIEW_ID')
        return self


class SigningLineage(VersionedProvenance):
    source_hashes: Annotated[list[SHA256], Field(min_length=1, max_length=5000)]
    actions: Annotated[list[SigningAction], Field(max_length=5000)]


class ProvenanceSignParams(VersionedProvenance):
    export: ArtifactRef
    lineage: ArtifactRef
    signing_policy_id: UUID
    policy: ArtifactRef

    @model_validator(mode='after')
    def refs(self):
        if self.signing_policy_id.int == 0 or any(r.artifact_id.int == 0 for r in (self.export, self.lineage, self.policy)):
            raise ValueError('PROVENANCE_PARAMS_REF')
        if len({r.artifact_id for r in (self.export, self.lineage, self.policy)}) != 3:
            raise ValueError('PROVENANCE_PARAMS_ALIAS')
        return self


class SignedResult(VersionedProvenance):
    signed_export: ArtifactRef
    manifest: ArtifactRef

    @model_validator(mode='after')
    def distinct_refs(self):
        if self.signed_export.artifact_id.int == 0 or self.manifest.artifact_id.int == 0 or self.signed_export.artifact_id == self.manifest.artifact_id:
            raise ValueError('PROVENANCE_RESULT_REF')
        return self


def decode_provenance(raw, contract, maximum):
    if not isinstance(raw, bytes) or not 2 <= len(raw) <= maximum:
        raise ValueError('PROVENANCE_DOCUMENT_SIZE')
    parsed = parse_json(raw, max_bytes=maximum)
    if canonical_bytes(parsed) != raw:
        raise ValueError('PROVENANCE_CANONICAL')
    result = contract.model_validate_json(raw)
    if canonical_bytes(result) != raw:
        raise ValueError('PROVENANCE_ROUNDTRIP')
    return result

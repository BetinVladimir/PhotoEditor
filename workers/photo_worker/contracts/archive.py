from __future__ import annotations

from typing import Annotated, Literal
from uuid import UUID
import unicodedata
from pydantic import Field, field_validator, model_validator
from .base import ArtifactRef, Contract, SHA256


class ArchiveEntry(Contract):
    artifact: ArtifactRef
    bytes: Annotated[int, Field(ge=1, le=10 * 1024**3)]
    entry_name: Annotated[str, Field(min_length=1, max_length=180)]

    @field_validator('entry_name')
    @classmethod
    def safe_name(cls, value: str) -> str:
        if value in ('.', '..') or '/' in value or '\\' in value or '\x00' in value:
            raise ValueError('Unsafe archive entry name')
        if any(ord(char) < 32 or ord(char) == 127 for char in value):
            raise ValueError('Unsafe archive entry name')
        if len(value.encode('utf-8')) > 180:
            raise ValueError('Archive entry name exceeds byte limit')
        if value != unicodedata.normalize('NFKC', value) or value.casefold() == 'manifest.json':
            raise ValueError('Archive entry name is not canonical')
        return value


class ArchiveParams(Contract):
    snapshot_id: UUID
    snapshot_digest: SHA256
    volume_no: Annotated[int, Field(ge=1, le=5000)]
    entries: Annotated[list[ArchiveEntry], Field(min_length=1, max_length=5000)]

    @model_validator(mode='after')
    def unique_and_bounded(self):
        names = [entry.entry_name.casefold() for entry in self.entries]
        if len(names) != len(set(names)):
            raise ValueError('Archive entry names collide')
        if sum(entry.bytes for entry in self.entries) > 10 * 1024**3:
            raise ValueError('Archive volume exceeds byte limit')
        return self


class ArchiveManifestEntry(Contract):
    ordinal: Annotated[int, Field(ge=0, le=4999)]
    entry_name: str
    artifact: ArtifactRef
    bytes: Annotated[int, Field(ge=1, le=10 * 1024**3)]
    sha256: SHA256


class ArchiveManifest(Contract):
    schema_version: Literal[1] = 1
    snapshot_id: UUID
    snapshot_digest: SHA256
    volume_no: Annotated[int, Field(ge=1, le=5000)]
    entries: Annotated[list[ArchiveManifestEntry], Field(min_length=1, max_length=5000)]
    archive_sha256: SHA256
    archive_bytes: Annotated[int, Field(ge=1)]

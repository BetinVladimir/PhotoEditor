"""Trusted, read-only evidence adapter for V2 adoption preparation."""
from __future__ import annotations

from contextlib import closing
from dataclasses import dataclass
from typing import Protocol
from uuid import UUID

from psycopg import Connection

from .adoption import AdoptionInvalid, ArtifactEvidence, ArtifactRef


class VersionedObjectStore(Protocol):
    def get_object(self, bucket_name: str, object_name: str, *, version_id: str): ...


@dataclass(frozen=True)
class _Location:
    bucket: str
    key: str
    version: str
    evidence: ArtifactEvidence


class PostgresObjectEvidenceReader:
    """Read immutable metadata on primary DB and exact object versions.

    The connection must be autocommit: object IO must never hold an adoption
    database transaction open. adopt_v2_plan later rechecks all metadata.
    """

    def __init__(self, connection: Connection, objects: VersionedObjectStore,
                 *, tenant_id: UUID, task_id: UUID, attempt: int, fence: int,
                 allowed_bucket: str, exposure: bool = False, editor_seal: bool = False,
                 shown_operation: str | None = None):
        if not connection.autocommit:
            raise ValueError("Adoption preparation requires an autocommit connection")
        if attempt < 1 or fence < 1:
            raise ValueError("Invalid task fence")
        if not allowed_bucket:
            raise ValueError("Missing adoption bucket")
        if type(exposure) is not bool:
            raise ValueError("Invalid adoption scope")
        if type(editor_seal) is not bool or editor_seal and exposure:raise ValueError("Invalid Editor seal scope")
        if shown_operation is not None and (shown_operation not in (
                'semantic.adopt_shown_exposure','semantic.adopt_shown_removal',
                'semantic.adopt_shown_background_cleanup','semantic.adopt_shown_retouch_region')
                or exposure or editor_seal):raise ValueError('Invalid shown adoption scope')
        self._shown_operation=shown_operation
        self._editor_seal = editor_seal
        self._exposure = exposure
        self.connection = connection
        self.objects = objects
        self.tenant_id = tenant_id
        self.task_id = task_id
        self.attempt = attempt
        self.fence = fence
        self.allowed_bucket = allowed_bucket
        self._locations: dict[ArtifactRef, _Location] = {}

    def _location(self, ref: ArtifactRef) -> _Location:
        if ref in self._locations:
            return self._locations[ref]
        row = self.connection.execute(
            "SELECT bucket,object_key,object_version,sha256,bytes,media_type,role,source "
            + ("FROM photo.v27_shown_adoption_artifact(%s,%s,%s,%s,%s,%s)" if self._shown_operation is not None else "FROM photo.editor_effect_seal_adoption_artifact(%s,%s,%s,%s,%s)" if self._editor_seal else "FROM photo.v27_exposure_adoption_artifact(%s,%s,%s,%s,%s)" if self._exposure
             else "FROM photo.v2_adoption_artifact(%s,%s,%s,%s,%s)"),
            (self.tenant_id, self.task_id, self.attempt, self.fence, ref.artifact_id)+((self._shown_operation,) if self._shown_operation is not None else ()),
        ).fetchone()
        if (row is None or row[0] != self.allowed_bucket or row[3] != ref.sha256 or row[4] < 0
                or not isinstance(row[1], str) or not row[1]
                or not isinstance(row[2], str) or row[2] in ("", "null")):
            raise AdoptionInvalid("ARTIFACT_EVIDENCE_MISMATCH")
        location = _Location(row[0], row[1], row[2],
                             ArtifactEvidence(ref, row[5], row[4], row[6], row[7]))
        self._locations[ref] = location
        return location

    def evidence(self, ref: ArtifactRef) -> ArtifactEvidence:
        return self._location(ref).evidence

    def read_json(self, ref: ArtifactRef, max_bytes: int) -> bytes:
        location = self._location(ref)
        if location.evidence.bytes > max_bytes or location.evidence.media_type != "application/json":
            raise AdoptionInvalid("ARTIFACT_JSON_TOO_LARGE")
        response = self.objects.get_object(location.bucket, location.key,
                                           version_id=location.version)
        try:
            with closing(response):
                data = bytearray()
                while len(data) <= location.evidence.bytes:
                    block = response.read(min(65536, location.evidence.bytes + 1 - len(data)))
                    if not isinstance(block, bytes):
                        raise AdoptionInvalid("ARTIFACT_STREAM_INVALID")
                    if not block:
                        break
                    data.extend(block)
                if len(data) != location.evidence.bytes:
                    raise AdoptionInvalid("ARTIFACT_SIZE_MISMATCH")
                return bytes(data)
        finally:
            release = getattr(response, "release_conn", None)
            if release is not None:
                release()

    def iter_bytes(self, ref: ArtifactRef, max_bytes: int):
        """Yield bounded blocks from the exact catalog object version."""
        location=self._location(ref)
        if type(max_bytes) is not int or max_bytes<1 or location.evidence.bytes>max_bytes:
            raise AdoptionInvalid('ARTIFACT_STREAM_TOO_LARGE')
        response=self.objects.get_object(location.bucket,location.key,version_id=location.version)
        try:
            with closing(response):
                count=0
                while True:
                    limit=min(65536,location.evidence.bytes+1-count)
                    block=response.read(limit)
                    if not isinstance(block,bytes) or len(block)>limit:
                        raise AdoptionInvalid('ARTIFACT_STREAM_INVALID')
                    if not block:break
                    count+=len(block)
                    if count>location.evidence.bytes:raise AdoptionInvalid('ARTIFACT_SIZE_MISMATCH')
                    yield block
                if count!=location.evidence.bytes:raise AdoptionInvalid('ARTIFACT_SIZE_MISMATCH')
        finally:
            release=getattr(response,'release_conn',None)
            if release is not None:release()

    def staged_outputs(self) -> set[ArtifactRef]:
        rows = self.connection.execute(
            ("SELECT artifact_id,sha256,state FROM photo.v27_shown_adoption_staged_outputs(%s,%s,%s,%s,%s)" if self._shown_operation is not None else "SELECT artifact_id,sha256,state FROM photo.editor_effect_seal_adoption_staged_outputs(%s,%s,%s,%s)" if self._editor_seal else "SELECT artifact_id,sha256,state FROM photo.v27_exposure_adoption_staged_outputs(%s,%s,%s,%s)" if self._exposure
             else "SELECT artifact_id,sha256,state FROM photo.v2_adoption_staged_outputs(%s,%s,%s,%s)"),
            (self.tenant_id, self.task_id, self.attempt, self.fence)+((self._shown_operation,) if self._shown_operation is not None else ()),
        ).fetchall()
        if any(state != "ready" for _, _, state in rows):
            raise AdoptionInvalid("STAGED_OUTPUT_INCOMPLETE")
        return {ArtifactRef(artifact_id, digest) for artifact_id, digest, _ in rows}

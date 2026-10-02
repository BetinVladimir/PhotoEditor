"""Disk-backed exact input allowlist for V2 task execution."""
from __future__ import annotations

from hashlib import sha256
from math import ceil
from sqlite3 import Connection
import sqlite3
from typing import Callable, Literal
from uuid import UUID

from pydantic import Field
from typing import Annotated

from photo_worker.contracts.base import ArtifactRef, Contract, SHA256, canonical_bytes
from photo_worker.runtime.parameters import read_contract
from photo_worker.runtime.scratch import Reservation, ScratchSpace
from .contract_lock import (SCHEMAS_SHA256, REPORT_PREVIOUS_SCHEMAS_SHA256, LENS_PREVIOUS_SCHEMAS_SHA256, INTERMEDIATE_SCHEMAS_SHA256,
                            PREVIOUS_SCHEMAS_SHA256)


class V2InputsInvalid(ValueError):
    pass


class InputClosureEntry(Contract):
    ref: ArtifactRef
    contract: Annotated[str, Field(min_length=1, max_length=120)]


class InputClosureChunk(Contract):
    schema_version: Literal[1]
    entries: Annotated[list[InputClosureEntry], Field(min_length=1, max_length=512)]


class InputClosureManifest(Contract):
    schema_version: Literal[1]
    task_id: UUID
    entry_count: Annotated[int, Field(ge=1, le=1_000_000)]
    entries_sha: SHA256
    chunks: Annotated[list[ArtifactRef], Field(min_length=1, max_length=1954)]


class V2InputIndex:
    def __init__(self, conn: Connection, reservation: Reservation):
        self._conn = conn
        self._reservation = reservation
        self._closed = False

    @classmethod
    def build(cls, *, task_id: UUID, inputs: list[ArtifactRef], params: ArtifactRef,
              execution_profile: dict, scratch: ScratchSpace,
              open_stream: Callable, check_active: Callable[[], None]) -> "V2InputIndex":
        if (execution_profile.get("renderer_version") != "v2.2"
                or execution_profile.get("contracts_sha") not in (
                    SCHEMAS_SHA256, REPORT_PREVIOUS_SCHEMAS_SHA256, LENS_PREVIOUS_SCHEMAS_SHA256, INTERMEDIATE_SCHEMAS_SHA256,
                    PREVIOUS_SCHEMAS_SHA256)
                or not 1 <= len(inputs) <= 128):
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        try:
            manifest_ref = ArtifactRef.model_validate_json(
                canonical_bytes(execution_profile["v2_input_manifest"]))
            if inputs.count(manifest_ref) != 1:
                raise V2InputsInvalid("TASK_INPUTS_INVALID")
            manifest = read_contract(manifest_ref, InputClosureManifest, open_stream, check_active)
            if (manifest.task_id != task_id or len(manifest.chunks) != ceil(manifest.entry_count / 512)
                    or execution_profile.get("v2_entry_count") != manifest.entry_count
                    or execution_profile.get("v2_entries_sha") != manifest.entries_sha):
                raise V2InputsInvalid("TASK_INPUTS_INVALID")
            chunk_ids = [ref.artifact_id for ref in manifest.chunks]
            if len(set(chunk_ids)) != len(chunk_ids) or manifest_ref.artifact_id in chunk_ids:
                raise V2InputsInvalid("TASK_INPUTS_INVALID")
        except (KeyError, TypeError, ValueError) as exc:
            raise V2InputsInvalid("TASK_INPUTS_INVALID") from exc
        reservation = scratch.reserve(max(16 * 1024 * 1024, manifest.entry_count * 512))
        conn = None
        try:
            path = reservation.path("input_index", ".bin")
            conn = sqlite3.connect(path)
            conn.execute("PRAGMA journal_mode=OFF")
            conn.execute("PRAGMA synchronous=OFF")
            conn.execute("PRAGMA temp_store=MEMORY")
            conn.execute("PRAGMA cache_size=-2048")
            conn.execute("CREATE TABLE allowed (artifact_id TEXT PRIMARY KEY,sha TEXT NOT NULL,"
                         "contract TEXT,is_entry INTEGER NOT NULL CHECK(is_entry IN(0,1)))")
            for ref in [*inputs, params, *manifest.chunks]:
                key = str(ref.artifact_id)
                old = conn.execute("SELECT sha FROM allowed WHERE artifact_id=?", (key,)).fetchone()
                if old is not None and old[0] != ref.sha256:
                    raise V2InputsInvalid("TASK_INPUTS_INVALID")
                conn.execute("INSERT OR IGNORE INTO allowed VALUES(?,?,NULL,0)", (key, ref.sha256))
            digest = sha256()
            digest.update(b"[")
            count = 0
            previous = None
            metadata = {manifest_ref.artifact_id, *(r.artifact_id for r in manifest.chunks)}
            for chunk_ref in manifest.chunks:
                check_active()
                try:
                    chunk = read_contract(chunk_ref, InputClosureChunk, open_stream, check_active)
                except ValueError as exc:
                    raise V2InputsInvalid("TASK_INPUTS_INVALID") from exc
                for entry in chunk.entries:
                    ref = entry.ref
                    if ref.artifact_id in metadata:
                        raise V2InputsInvalid("TASK_INPUTS_INVALID")
                    order = (ref.artifact_id.bytes, ref.sha256, entry.contract)
                    if previous is not None and order <= previous:
                        raise V2InputsInvalid("TASK_INPUTS_INVALID")
                    previous = order
                    if count:
                        digest.update(b",")
                    digest.update(canonical_bytes(entry))
                    count += 1
                    key = str(ref.artifact_id)
                    old = conn.execute("SELECT sha,is_entry FROM allowed WHERE artifact_id=?", (key,)).fetchone()
                    if old is None:
                        conn.execute("INSERT INTO allowed VALUES(?,?,?,1)", (key, ref.sha256, entry.contract))
                    elif old[0] != ref.sha256 or old[1] == 1:
                        raise V2InputsInvalid("TASK_INPUTS_INVALID")
                    else:
                        conn.execute("UPDATE allowed SET contract=?,is_entry=1 WHERE artifact_id=?",
                                     (entry.contract, key))
                check_active()
            digest.update(b"]")
            if count != manifest.entry_count or digest.hexdigest() != manifest.entries_sha:
                raise V2InputsInvalid("TASK_INPUTS_INVALID")
            conn.commit()
            reservation.validate()
            conn.execute("PRAGMA query_only=ON")
            return cls(conn, reservation)
        except BaseException:
            if conn is not None:
                conn.close()
            reservation.release()
            raise

    def allows(self, ref: ArtifactRef) -> bool:
        if self._closed:
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        row = self._conn.execute("SELECT sha FROM allowed WHERE artifact_id=?",
                                 (str(ref.artifact_id),)).fetchone()
        return row is not None and row[0] == ref.sha256

    def refs_for_contract(self, contract: str, maximum: int = 1) -> tuple[ArtifactRef, ...]:
        """Bounded typed discovery from the verified immutable closure only."""
        if self._closed or not isinstance(contract, str) or not 1 <= len(contract) <= 120 or type(maximum) is not int or not 1 <= maximum <= 512:
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        rows = self._conn.execute("SELECT artifact_id,sha FROM allowed WHERE contract=? AND is_entry=1 ORDER BY artifact_id LIMIT ?",
                                  (contract, maximum + 1)).fetchall()
        if len(rows) > maximum:
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        return tuple(ArtifactRef(artifact_id=UUID(row[0]), sha256=row[1]) for row in rows)

    def close(self) -> None:
        if not self._closed:
            self._closed = True
            self._conn.close()
            self._reservation.release()

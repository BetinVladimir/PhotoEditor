"""Trusted compiler-side streaming construction of a V2 input closure.

The caller supplies an exact, UUID-ordered artifact cursor and persists every
returned row in the same admission transaction as the task. Object writes must
be immutable and orphan-safe if that transaction rolls back.
"""
from __future__ import annotations

from dataclasses import dataclass
from hashlib import sha256
from typing import Callable, Iterable
from uuid import UUID

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from .input_closure import InputClosureChunk, InputClosureEntry, InputClosureManifest, V2InputsInvalid


@dataclass(frozen=True)
class BuiltInputClosure:
    manifest: ArtifactRef
    chunks: tuple[ArtifactRef, ...]
    entry_count: int


def build_input_closure_from_sorted(
    task_id: UUID,
    entries: Iterable[InputClosureEntry],
    upload_json: Callable[[bytes, str], ArtifactRef],
    persist_rows: Callable[[int, list[InputClosureEntry]], None],
    persist_transport: Callable[[ArtifactRef, str, int], None],
) -> BuiltInputClosure:
    """Build chunks with bounded RAM; input cursor must be sorted by UUID/SHA/contract.

    `persist_rows` receives ≤512 entries with the first ordinal. The caller owns
    the admission transaction and must roll it back on any exception. Metadata
    rows use ordinals 1_000_000..1_001_954, disjoint from entry ordinals.
    """
    if not isinstance(task_id, UUID):
        raise TypeError("task_id must be UUID")
    digest = sha256()
    digest.update(b"[")
    count = 0
    chunks: list[ArtifactRef] = []
    batch: list[InputClosureEntry] = []
    previous = None

    def upload(value, contract: str) -> ArtifactRef:
        raw = canonical_bytes(value)
        ref = upload_json(raw, contract)
        if not isinstance(ref, ArtifactRef) or ref.sha256 != sha256(raw).hexdigest():
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        return ref

    def flush() -> None:
        if not batch:
            return
        first = count - len(batch)
        chunk_ref = upload(InputClosureChunk(schema_version=1, entries=list(batch)), "InputClosureChunk")
        persist_rows(first, list(batch))
        persist_transport(chunk_ref, "InputClosureChunk", 1_000_000 + len(chunks))
        chunks.append(chunk_ref)
        batch.clear()

    for entry in entries:
        if not isinstance(entry, InputClosureEntry):
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        order = (entry.ref.artifact_id.bytes, entry.ref.sha256, entry.contract)
        if previous is not None and (order <= previous or order[0] == previous[0]):
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        if count >= 1_000_000:
            raise V2InputsInvalid("TASK_INPUTS_INVALID")
        previous = order
        if count:
            digest.update(b",")
        digest.update(canonical_bytes(entry))
        batch.append(entry)
        count += 1
        if len(batch) == 512:
            flush()
    if count == 0:
        raise V2InputsInvalid("TASK_INPUTS_INVALID")
    flush()
    digest.update(b"]")
    manifest = upload(InputClosureManifest(schema_version=1, task_id=task_id,
                                           entry_count=count, entries_sha=digest.hexdigest(),
                                           chunks=chunks), "InputClosureManifest")
    ids = {chunk.artifact_id for chunk in chunks}
    if (len(ids) != len(chunks) or manifest.artifact_id in ids
            or any(chunk.artifact_id == manifest.artifact_id for chunk in chunks)):
        raise V2InputsInvalid("TASK_INPUTS_INVALID")
    persist_transport(manifest, "InputClosureManifest", 1_000_000 + len(chunks))
    return BuiltInputClosure(manifest, tuple(chunks), count)

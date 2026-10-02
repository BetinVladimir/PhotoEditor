"""Exact, bounded V2 input closure used by the worker before adapter execution."""
from hashlib import sha256
from io import BytesIO
from pathlib import Path
from uuid import uuid4

import pytest

from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.runtime.scratch import ScratchSpace
from photo_worker.v2.contract_lock import SCHEMAS_SHA256
from photo_worker.v2.input_closure import (
    InputClosureChunk, InputClosureEntry, InputClosureManifest,
    V2InputIndex, V2InputsInvalid,
)
from photo_worker.v2.input_closure_builder import build_input_closure_from_sorted


def fixture(tmp_path, *, reverse=False, wrong_digest=False):
    task_id = uuid4()
    tenant_id = uuid4()
    bodies = {}

    def put(value):
        raw = canonical_bytes(value)
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
        bodies[ref.artifact_id] = raw
        return ref

    entries = sorted(
        [InputClosureEntry(ref=ArtifactRef(artifact_id=uuid4(), sha256="a" * 64), contract="image/tiff"),
         InputClosureEntry(ref=ArtifactRef(artifact_id=uuid4(), sha256="b" * 64), contract="image/tiff")],
        key=lambda e: (e.ref.artifact_id.bytes, e.ref.sha256, e.contract),
        reverse=reverse,
    )
    chunk = put(InputClosureChunk(schema_version=1, entries=entries))
    digest = sha256(canonical_bytes([entry.model_dump(mode="json") for entry in entries])).hexdigest()
    manifest = put(InputClosureManifest(schema_version=1, task_id=task_id, entry_count=2,
                                         entries_sha=("0" * 64 if wrong_digest else digest), chunks=[chunk]))
    params = ArtifactRef(artifact_id=uuid4(), sha256="c" * 64)
    scratch = ScratchSpace(tmp_path, tenant_id, task_id, 1, 32 * 1024 * 1024)
    profile = {"renderer_version": "v2.2", "contracts_sha": SCHEMAS_SHA256,
               "v2_input_manifest": manifest.model_dump(mode="json"),
               "v2_entry_count": 2, "v2_entries_sha": "0" * 64 if wrong_digest else digest}

    def open_stream(ref):
        return BytesIO(bodies[ref.artifact_id])

    return task_id, entries, manifest, params, scratch, profile, open_stream


def test_exact_disk_backed_allowlist_and_cleanup(tmp_path):
    task, entries, manifest, params, scratch, profile, open_stream = fixture(tmp_path)
    try:
        index = V2InputIndex.build(task_id=task, inputs=[manifest, entries[0].ref], params=params,
                                   execution_profile=profile, scratch=scratch,
                                   open_stream=open_stream, check_active=lambda: None)
        assert index.allows(entries[0].ref)
        assert index.allows(entries[1].ref)
        assert index.allows(params)
        assert index.refs_for_contract('image/tiff',maximum=2)==tuple(entry.ref for entry in entries)
        assert index.refs_for_contract('OriginalLensBinding')==()
        with pytest.raises(V2InputsInvalid):index.refs_for_contract('image/tiff',maximum=1)
        with pytest.raises(V2InputsInvalid):index.refs_for_contract('image/tiff',maximum=True)
        assert not index.allows(ArtifactRef(artifact_id=entries[1].ref.artifact_id, sha256="d" * 64))
        assert not index.allows(ArtifactRef(artifact_id=uuid4(), sha256="d" * 64))
        assert len(list(scratch.directory.glob("input_index-*.bin"))) == 1
        index.close()
        with pytest.raises(V2InputsInvalid):index.refs_for_contract('image/tiff',maximum=2)
        assert not list(scratch.directory.iterdir())
    finally:
        scratch.cleanup()


@pytest.mark.parametrize("reverse,wrong_digest", [(True, False), (False, True)])
def test_order_and_digest_failure_release_index(tmp_path, reverse, wrong_digest):
    task, entries, manifest, params, scratch, profile, open_stream = fixture(
        tmp_path, reverse=reverse, wrong_digest=wrong_digest)
    try:
        with pytest.raises(V2InputsInvalid, match="TASK_INPUTS_INVALID"):
            V2InputIndex.build(task_id=task, inputs=[manifest], params=params,
                               execution_profile=profile, scratch=scratch,
                               open_stream=open_stream, check_active=lambda: None)
        assert not list(scratch.directory.iterdir())
    finally:
        scratch.cleanup()


def test_manifest_must_be_direct_and_locked(tmp_path):
    task, entries, manifest, params, scratch, profile, open_stream = fixture(tmp_path)
    try:
        with pytest.raises(V2InputsInvalid):
            V2InputIndex.build(task_id=task, inputs=[entries[0].ref], params=params,
                               execution_profile=profile, scratch=scratch,
                               open_stream=open_stream, check_active=lambda: None)
        with pytest.raises(V2InputsInvalid):
            V2InputIndex.build(task_id=task, inputs=[manifest], params=params,
                               execution_profile={**profile, "contracts_sha": "0" * 64}, scratch=scratch,
                               open_stream=open_stream, check_active=lambda: None)
        with pytest.raises(V2InputsInvalid):
            V2InputIndex.build(task_id=task, inputs=[manifest], params=params,
                               execution_profile={**profile, "v2_entry_count": 3}, scratch=scratch,
                               open_stream=open_stream, check_active=lambda: None)
    finally:
        scratch.cleanup()


def test_tampered_chunk_maps_to_input_protocol_error(tmp_path):
    task, entries, manifest, params, scratch, profile, open_stream = fixture(tmp_path)
    chunk_id = InputClosureManifest.model_validate_json(open_stream(manifest).read()).chunks[0].artifact_id

    def tampered(ref):
        if ref.artifact_id == chunk_id:
            return BytesIO(b"{}")
        return open_stream(ref)

    try:
        with pytest.raises(V2InputsInvalid, match="TASK_INPUTS_INVALID"):
            V2InputIndex.build(task_id=task, inputs=[manifest], params=params,
                               execution_profile=profile, scratch=scratch,
                               open_stream=tampered, check_active=lambda: None)
        assert not list(scratch.directory.iterdir())
    finally:
        scratch.cleanup()


def test_contract_lock_matches_schema_file():
    root = Path(__file__).resolve().parents[2]
    assert sha256((root / "docs/implementation-v2/contracts/schemas.json").read_bytes()).hexdigest() == SCHEMAS_SHA256


def test_compiler_builder_round_trip_across_chunk_boundary(tmp_path):
    task = uuid4()
    entries = sorted(
        (InputClosureEntry(ref=ArtifactRef(artifact_id=uuid4(), sha256="a" * 64),
                           contract="image/tiff") for _ in range(513)),
        key=lambda e: e.ref.artifact_id.bytes,
    )
    bodies = {}
    rows = []
    transport = []

    def upload(raw, contract):
        ref = ArtifactRef(artifact_id=uuid4(), sha256=sha256(raw).hexdigest())
        bodies[ref.artifact_id] = raw
        return ref

    built = build_input_closure_from_sorted(
        task, entries, upload,
        lambda first, batch: rows.append((first, len(batch))),
        lambda ref, contract, ordinal: transport.append((contract, ordinal)),
    )
    assert built.entry_count == 513
    assert rows == [(0, 512), (512, 1)]
    assert transport == [("InputClosureChunk", 1_000_000),
                         ("InputClosureChunk", 1_000_001),
                         ("InputClosureManifest", 1_000_002)]
    scratch = ScratchSpace(tmp_path, uuid4(), task, 1, 32 * 1024 * 1024)
    try:
        profile = {"renderer_version": "v2.2", "contracts_sha": SCHEMAS_SHA256,
                   "v2_input_manifest": built.manifest.model_dump(mode="json"),
                   "v2_entry_count": built.entry_count,
                   "v2_entries_sha": InputClosureManifest.model_validate_json(
                       bodies[built.manifest.artifact_id]).entries_sha}
        index = V2InputIndex.build(
            task_id=task, inputs=[built.manifest],
            params=ArtifactRef(artifact_id=uuid4(), sha256="b" * 64),
            execution_profile=profile, scratch=scratch,
            open_stream=lambda ref: BytesIO(bodies[ref.artifact_id]), check_active=lambda: None)
        assert all(index.allows(entry.ref) for entry in entries)
        index.close()
    finally:
        scratch.cleanup()


def test_compiler_rejects_reused_artifact_id_even_with_other_hash():
    artifact_id = uuid4()
    entries = [
        InputClosureEntry(ref=ArtifactRef(artifact_id=artifact_id, sha256=digest * 64),
                          contract="image/tiff")
        for digest in ("a", "b")
    ]
    with pytest.raises(V2InputsInvalid, match="TASK_INPUTS_INVALID"):
        build_input_closure_from_sorted(uuid4(), entries,
                                        lambda *_: None, lambda *_: None, lambda *_: None)


def test_prior_lens_schema_pin_keeps_existing_jobs_readable(tmp_path):
    from photo_worker.v2.contract_lock import LENS_PREVIOUS_SCHEMAS_SHA256
    task,entries,manifest,params,scratch,profile,open_stream=fixture(tmp_path)
    profile['contracts_sha']=LENS_PREVIOUS_SCHEMAS_SHA256
    try:
        index=V2InputIndex.build(task_id=task,inputs=[manifest],params=params,
            execution_profile=profile,scratch=scratch,open_stream=open_stream,check_active=lambda:None)
        assert index.allows(entries[0].ref)
        index.close()
    finally:scratch.cleanup()

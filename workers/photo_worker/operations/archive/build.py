"""Bounded streaming ZIP64 builder for an already planned download volume."""
from __future__ import annotations

import hashlib
import json
from pathlib import Path
import zipfile
from pydantic import ValidationError
from photo_worker.contracts.archive import ArchiveManifest, ArchiveManifestEntry, ArchiveParams
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def _copy_entry(ctx: ExecutionContext, source, target, expected_bytes: int, expected_sha: str) -> str:
    digest = hashlib.sha256()
    total = 0
    while True:
        ctx.check_active()
        limit = min(1024 * 1024, expected_bytes - total + 1)
        block = source.read(limit)
        ctx.check_active()
        if not isinstance(block, bytes) or len(block) > limit:
            raise OperationFailure('ARTIFACT_HASH_MISMATCH')
        if not block:
            break
        total += len(block)
        if total > expected_bytes:
            raise OperationFailure('ARTIFACT_HASH_MISMATCH')
        written = target.write(block)
        if type(written) is not int or written != len(block):
            raise OperationFailure('INTERNAL_ERROR')
        ctx.check_active()
        digest.update(block)
    if total != expected_bytes or digest.hexdigest() != expected_sha:
        raise OperationFailure('ARTIFACT_HASH_MISMATCH')
    return digest.hexdigest()


class _Discard:
    def write(self, block):
        return len(block)


def _verify_archive(ctx, path, entries, manifest_payload):
    """CRC + SHA verification remains cancellable even for multi-GiB members."""
    try:
        with zipfile.ZipFile(path, mode='r') as archive:
            expected=[entry.entry_name for entry in entries]+['manifest.json']
            if archive.namelist()!=expected:
                raise OperationFailure('INTERNAL_ERROR')
            for entry in entries:
                ctx.check_active()
                info=archive.getinfo(entry.entry_name)
                if info.file_size!=entry.bytes or info.compress_type!=zipfile.ZIP_STORED:
                    raise OperationFailure('INTERNAL_ERROR')
                with archive.open(info) as source:
                    _copy_entry(ctx,source,_Discard(),entry.bytes,entry.artifact.sha256)
            with archive.open('manifest.json') as source:
                _copy_entry(ctx,source,_Discard(),len(manifest_payload),hashlib.sha256(manifest_payload).hexdigest())
    except zipfile.BadZipFile as exc:
        raise OperationFailure('INTERNAL_ERROR') from exc


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    if ctx.lease.execution_profile.get('backend') != 'cpu':
        raise OperationFailure('CONFIG_INVALID')
    try:
        ctx.check_active()
        params = artifacts.params(ArchiveParams).model_copy(deep=True)
        ctx.check_active()
    except (ValueError, ValidationError) as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    declared = list(ctx.lease.command.inputs)
    if len(declared) != len(params.entries) or any(entry.artifact != declared[index] for index, entry in enumerate(params.entries)):
        raise OperationFailure('INPUT_INVALID')
    estimate = sum(entry.bytes for entry in params.entries)
    # Stored payload needs no percentage expansion. Reserve per-entry ZIP64
    # headers/names and the bounded JSON manifest as well: 5000 tiny images
    # otherwise overflow the old fixed 1MiB minimum despite valid admission.
    reserve_bytes = estimate + 1024 * 1024 + 4096 * len(params.entries)
    manifest_entries = [ArchiveManifestEntry(ordinal=ordinal, entry_name=entry.entry_name,
        artifact=entry.artifact.model_copy(deep=True), bytes=entry.bytes, sha256=entry.artifact.sha256)
        for ordinal, entry in enumerate(params.entries)]
    # Check ordinary archive_manifest's 1MiB contract before image IO. The
    # reserved-size spelling bounds actual archive_bytes in the final document.
    manifest_bound = ArchiveManifest(snapshot_id=params.snapshot_id, snapshot_digest=params.snapshot_digest,
        volume_no=params.volume_no, entries=manifest_entries, archive_sha256='0' * 64,
        archive_bytes=reserve_bytes)
    if len(canonical_bytes(manifest_bound)) > 1048576:
        raise OperationFailure('INPUT_INVALID')
    del manifest_bound
    ctx.check_active()
    total_steps = len(params.entries) + 2
    ctx.progress(0, total_steps)
    with artifacts.scratch.reserve(reserve_bytes) as reservation:
        archive_path = reservation.path('archive', '.zip')
        with zipfile.ZipFile(archive_path, mode='w', compression=zipfile.ZIP_STORED, allowZip64=True) as archive:
            for ordinal, entry in enumerate(params.entries):
                info = zipfile.ZipInfo(entry.entry_name)
                info.compress_type = zipfile.ZIP_STORED
                info.external_attr = 0o600 << 16
                info.date_time = (1980, 1, 1, 0, 0, 0)
                with artifacts.stream(entry.artifact) as source, archive.open(info, mode='w', force_zip64=True) as target:
                    _copy_entry(ctx, source, target, entry.bytes, entry.artifact.sha256)
                ctx.progress(ordinal + 1, total_steps)
            provisional = dict(schema_version=1, snapshot_id=str(params.snapshot_id),
                snapshot_digest=params.snapshot_digest, volume_no=params.volume_no,
                entries=[entry.model_dump(mode='json') for entry in manifest_entries])
            manifest_payload=json.dumps(provisional, ensure_ascii=False, sort_keys=True,
                separators=(',', ':')).encode('utf-8')
            archive.writestr('manifest.json',manifest_payload,compress_type=zipfile.ZIP_DEFLATED)
        reservation.validate_file(archive_path)
        archive_hash = hashlib.sha256()
        archive_bytes = 0
        with Path(archive_path).open('rb') as source:
            for block in iter(lambda: source.read(1024 * 1024), b''):
                ctx.check_active()
                archive_hash.update(block)
                archive_bytes += len(block)
        _verify_archive(ctx,archive_path,params.entries,manifest_payload)
        ctx.check_active()
        archive_ref = artifacts.stage(archive_path, reservation, 'archive', 'application/zip')
        ctx.check_active()
        ctx.progress(total_steps - 1, total_steps)
    manifest = ArchiveManifest(snapshot_id=params.snapshot_id, snapshot_digest=params.snapshot_digest,
        volume_no=params.volume_no, entries=manifest_entries, archive_sha256=archive_hash.hexdigest(),
        archive_bytes=archive_bytes)
    ctx.check_active()
    manifest_ref = artifacts.stage_contract(manifest, 'archive_manifest')
    ctx.check_active()
    ctx.progress(total_steps, total_steps)
    return OperationResult(outputs=[archive_ref, manifest_ref], bytes_read=estimate,
        bytes_written=archive_bytes + len(canonical_bytes(manifest)))


def archive_operations(config: ArtifactConfig) -> list[Operation]:
    def build(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'archive', 'archive_manifest'})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation('archive.build', 'archive', build)]

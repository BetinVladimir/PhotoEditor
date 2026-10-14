"""Strict exact-version delivery archive manifest for one encoded fixture item."""
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,parse_json
from photo_worker.contracts.archive import ArchiveManifest,ArchiveEntry
from .restore_objects import MinioRestoreObjects
from .restore_export_volume import read_fixture_export_volume
from .restore_authority import require_restore_active


def validate_fixture_archive_manifest(raw,target,selection,encoded,volume,*,check_active):
    if type(raw) is not bytes or not 2<=len(raw)<=1048576 or volume.delivery_kind!='zip' or not callable(check_active):
        raise ValueError('RESTORE_ARCHIVE_MANIFEST_INSTALLATION')
    require_restore_active(check_active);manifest=ArchiveManifest.model_validate_json(raw)
    if canonical_bytes(parse_json(raw))!=raw or canonical_bytes(manifest)!=raw:
        raise ValueError('RESTORE_ARCHIVE_MANIFEST_CANONICAL')
    ref=ArtifactRef(artifact_id=encoded.artifact_id,sha256=encoded.object.sha256)
    entry=ArchiveEntry(artifact=ref,bytes=encoded.object.size,entry_name=encoded.entry_name)
    if (manifest.snapshot_id!=target.selection_snapshot_id or manifest.snapshot_digest!=selection.snapshot_digest
            or manifest.volume_no!=encoded.volume_no or manifest.archive_sha256!=volume.object.sha256
            or manifest.archive_bytes!=volume.object.size or len(manifest.entries)!=1):
        raise ValueError('RESTORE_ARCHIVE_MANIFEST_BINDING')
    actual=manifest.entries[0]
    if (actual.ordinal!=0 or actual.entry_name!=entry.entry_name or actual.artifact!=ref
            or actual.bytes!=entry.bytes or actual.sha256!=ref.sha256):
        raise ValueError('RESTORE_ARCHIVE_MANIFEST_ENTRY')
    if sha256(raw).hexdigest()!=volume.manifest_object.sha256 or len(raw)!=volume.manifest_object.size:
        raise ValueError('RESTORE_ARCHIVE_MANIFEST_SHA')
    require_restore_active(check_active);return manifest


def verify_fixture_archive_manifest(connection,client,buckets,target,selection,encoded,*,expected_database,check_active):
    require_restore_active(check_active)
    def observe():return read_fixture_export_volume(connection,target.tenant_id,encoded,
        expected_database=expected_database,check_active=check_active)
    volume=observe()
    if volume.delivery_kind!='zip' or volume.manifest_object is None:
        raise ValueError('RESTORE_ARCHIVE_MANIFEST_REQUIRED')
    obj=volume.manifest_object
    if not 2<=obj.size<=1048576:raise ValueError('RESTORE_ARCHIVE_MANIFEST_SIZE')
    store=MinioRestoreObjects(client,buckets,check_active=check_active)
    with store.open_version(obj) as stream:
        chunks=[];size=0
        while True:
            require_restore_active(check_active);block=stream.read(min(65536,obj.size-size+1))
            if not block:break
            size+=len(block)
            if size>obj.size:raise ValueError('RESTORE_ARCHIVE_MANIFEST_SIZE')
            chunks.append(block)
    manifest=validate_fixture_archive_manifest(b''.join(chunks),target,selection,encoded,volume,check_active=check_active)
    if observe()!=volume:raise ValueError('RESTORE_ARCHIVE_MANIFEST_CHANGED')
    require_restore_active(check_active);return volume,manifest

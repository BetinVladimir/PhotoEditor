"""Exact-version ZIP bytes plus production cancellable entry/CRC/SHA verifier."""
from dataclasses import dataclass
from hashlib import sha256
from pathlib import Path
from tempfile import TemporaryDirectory
from uuid import uuid4
from .scratch import ScratchSpace
from types import SimpleNamespace
import json
from photo_worker.contracts.archive import ArchiveEntry
from photo_worker.operations.archive.build import _verify_archive
from .restore_objects import MinioRestoreObjects
from .restore_archive_manifest import verify_fixture_archive_manifest
from .restore_export_volume import read_fixture_export_volume
from .restore_authority import require_restore_active


@dataclass(frozen=True)
class RestorePhysicalArchive:
    volume: object
    archive_sha256: str
    bytes_verified: int
    encoded_sha256: str
    manifest_sha256: str


def verify_fixture_archive(connection,client,buckets,target,selection,encoded,*,expected_database,scratch_root,check_active):
    root=Path(scratch_root)
    if not callable(check_active):
        raise ValueError('RESTORE_ARCHIVE_SCRATCH_INSTALLATION')
    require_restore_active(check_active)
    if not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir():
        raise ValueError('RESTORE_ARCHIVE_SCRATCH_INSTALLATION')
    volume,manifest=verify_fixture_archive_manifest(connection,client,buckets,target,selection,encoded,
        expected_database=expected_database,check_active=check_active)
    obj=volume.object
    if not 1<=obj.size<=10737418240:raise ValueError('RESTORE_ARCHIVE_SIZE')
    entries=[ArchiveEntry(artifact=e.artifact,bytes=e.bytes,entry_name=e.entry_name) for e in manifest.entries]
    provisional={'schema_version':1,'snapshot_id':str(manifest.snapshot_id),'snapshot_digest':manifest.snapshot_digest,
        'volume_no':manifest.volume_no,'entries':[e.model_dump(mode='json') for e in manifest.entries]}
    payload=json.dumps(provisional,ensure_ascii=False,sort_keys=True,separators=(',',':')).encode('utf-8')
    store=MinioRestoreObjects(client,buckets,check_active=check_active)
    with TemporaryDirectory(prefix='restore-archive-',dir=root) as directory, \
            ScratchSpace(Path(directory),target.tenant_id,uuid4(),1,obj.size) as scratch, \
            scratch.reserve(obj.size) as reservation:
        path=reservation.planned_path('volume','.zip');digest=sha256();size=0
        with path.open('xb') as output,store.open_version(obj) as stream:
            while True:
                require_restore_active(check_active);block=stream.read(min(65536,obj.size-size+1))
                if not block:break
                size+=len(block)
                if size>obj.size:raise ValueError('RESTORE_ARCHIVE_SIZE')
                digest.update(block)
                if output.write(block)!=len(block):raise OSError('RESTORE_ARCHIVE_SHORT_WRITE')
        reservation.validate_file(path)
        if size!=obj.size or digest.hexdigest()!=obj.sha256:raise ValueError('RESTORE_ARCHIVE_SHA')
        require_restore_active(check_active);_verify_archive(SimpleNamespace(check_active=check_active),path,entries,payload);require_restore_active(check_active)
        if read_fixture_export_volume(connection,target.tenant_id,encoded,
                expected_database=expected_database,check_active=check_active)!=volume:
            raise ValueError('RESTORE_ARCHIVE_CHANGED')
    require_restore_active(check_active);return RestorePhysicalArchive(volume,obj.sha256,size,encoded.object.sha256,volume.manifest_object.sha256)

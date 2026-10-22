"""Portable, verified independent deletion-history backup snapshots.

A successful export is a local artifact, not proof of off-host replication or
that this is the latest required recovery watermark. Operators must durably
publish the returned manifest SHA/checkpoint through their backup authority.
"""
from dataclasses import dataclass
from hashlib import sha256
import os
from pathlib import Path
import shutil
import sqlite3
import tempfile
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json
from .deletion_history_store import DeletionHistoryStore
from .restore_deletions import DeletionHistoryCheckpoint,verify_restore_deletions
from .atomic_directory import (directory_identity as _directory_identity,
                               rename_exclusive as _rename_exclusive,
                               sync_directory as _sync_directory)


@dataclass(frozen=True)
class DeletionHistoryBackup:
    directory: Path
    manifest_sha256: str
    database_sha256: str
    checkpoint: DeletionHistoryCheckpoint


def export_deletion_history_backup(store,destination_root,backup_id,*,check_active,
                                   maximum_bytes=1<<30):
    root=Path(destination_root)
    if (not isinstance(store,DeletionHistoryStore) or not isinstance(backup_id,UUID) or backup_id.int==0
            or not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir()
            or root.stat().st_mode & 0o022 or not callable(check_active)
            or type(maximum_bytes) is not int or not 65536<=maximum_bytes<=128<<30):
        raise ValueError('DELETION_BACKUP_INSTALLATION')
    final=root/str(backup_id)
    root_identity=_directory_identity(root)
    if final.exists():raise ValueError('DELETION_BACKUP_EXISTS')
    check_active();pending=Path(tempfile.mkdtemp(prefix='.deletion-backup-',dir=root))
    try:
        path=pending/'history.sqlite'
        descriptor=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL,0o600);os.close(descriptor)
        source=store._connect()
        try:
            target=sqlite3.connect(path,isolation_level=None)
            try:
                page_size=source.execute('PRAGMA page_size').fetchone()[0]
                def progress(status,remaining,total):
                    check_active()
                    if total*page_size>maximum_bytes:raise ValueError('DELETION_BACKUP_LIMIT')
                source.backup(target,pages=128,progress=progress,sleep=.01)
                target.execute('PRAGMA journal_mode=DELETE')
                target.execute('PRAGMA query_only=ON');target.execute('BEGIN')
                last=target.execute('SELECT sequence,sha256 FROM events ORDER BY sequence DESC LIMIT 1').fetchone()
                checkpoint=DeletionHistoryCheckpoint(0,'0'*64) if last is None else DeletionHistoryCheckpoint(*last)
                def entries():
                    for body,digest in target.execute('SELECT body,sha256 FROM events ORDER BY sequence'):
                        check_active()
                        if sha256(body).hexdigest()!=digest:raise ValueError('DELETION_BACKUP_CORRUPT')
                        value=parse_json(body,max_bytes=4096)
                        if canonical_bytes(value)!=body:raise ValueError('DELETION_BACKUP_CORRUPT')
                        yield value
                verify_restore_deletions(entries(),checkpoint,lambda _:None,check_active=check_active)
            finally:target.close()
        finally:source.close()
        digest=sha256();size=0
        with path.open('rb') as stream:
            while True:
                check_active();chunk=stream.read(1<<20)
                if not chunk:break
                size+=len(chunk)
                if size>maximum_bytes:raise ValueError('DELETION_BACKUP_LIMIT')
                digest.update(chunk)
            os.fsync(stream.fileno())
        manifest=canonical_bytes({'schema_version':'deletion-history-backup/v1',
            'backup_id':str(backup_id),'database_sha256':digest.hexdigest(),'database_bytes':size,
            'sequence':checkpoint.sequence,'head_sha256':checkpoint.head_sha256})
        with (pending/'manifest.json').open('xb') as stream:
            os.chmod(pending/'manifest.json',0o600)
            stream.write(manifest);stream.flush();os.fsync(stream.fileno())
        _sync_directory(pending);check_active()
        if _directory_identity(root)!=root_identity:
            raise ValueError('DELETION_BACKUP_ROOT_CHANGED')
        if set(os.listdir(pending))!={'history.sqlite','manifest.json'}:
            raise ValueError('DELETION_BACKUP_CLOSURE')
        if final.exists():raise ValueError('DELETION_BACKUP_EXISTS')
        root_fd=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        try:
            observed=os.fstat(root_fd)
            if ((observed.st_dev,observed.st_ino)!=root_identity
                    or _directory_identity(root)!=root_identity):
                raise ValueError('DELETION_BACKUP_ROOT_CHANGED')
            _rename_exclusive(root_fd,pending.name,final.name)
            os.fsync(root_fd)
            if _directory_identity(root)!=root_identity:
                raise ValueError('DELETION_BACKUP_ROOT_CHANGED')
        finally:os.close(root_fd)
        return DeletionHistoryBackup(final,sha256(manifest).hexdigest(),digest.hexdigest(),checkpoint)
    finally:
        # Never traverse a replacement root during failure cleanup. The original
        # private pending directory remains available for operator recovery.
        if _directory_identity(root)==root_identity and pending.exists():shutil.rmtree(pending)

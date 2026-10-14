"""Read a pinned portable history backup against independent latest watermark."""
from contextlib import contextmanager
from hashlib import sha256
import os
from pathlib import Path
import sqlite3
import stat
from urllib.parse import quote
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_deletions import DeletionHistoryCheckpoint,_sha
from .restore_deletion_catalogue import verify_restore_database_deletions


def _file_digest(path,maximum,active):
    info=path.lstat()
    if (not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or info.st_mode & 0o077
            or info.st_uid not in (0,os.geteuid())
            or not 1<=info.st_size<=maximum):raise ValueError('DELETION_BACKUP_FILE')
    descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:
        opened=os.fstat(descriptor)
        if (opened.st_dev,opened.st_ino)!=(info.st_dev,info.st_ino):raise ValueError('DELETION_BACKUP_CHANGED')
        digest=sha256();received=0
        while True:
            active();chunk=os.read(descriptor,1<<20)
            if not chunk:break
            received+=len(chunk)
            if received>maximum:raise ValueError('DELETION_BACKUP_LIMIT')
            digest.update(chunk)
        after=os.fstat(descriptor);current=path.lstat()
        identity=lambda value:(value.st_dev,value.st_ino,value.st_size,value.st_mtime_ns,value.st_ctime_ns,value.st_mode,value.st_nlink)
        if identity(info)!=identity(after) or identity(after)!=identity(current) or received!=info.st_size:
            raise ValueError('DELETION_BACKUP_CHANGED')
        active();return digest.hexdigest(),received
    finally:os.close(descriptor)


@contextmanager
def open_deletion_history_backup(directory,manifest_sha,latest_checkpoint,*,check_active,
                                 maximum_database_bytes=1<<30):
    root=Path(directory)
    try:root_info=root.lstat() if root.is_absolute() else None
    except OSError:root_info=None
    if (root_info is None or not stat.S_ISDIR(root_info.st_mode)
            or root_info.st_mode & 0o077 or root_info.st_uid not in (0,os.geteuid())
            or root.resolve(strict=True)!=root
            or not _sha(manifest_sha) or not isinstance(latest_checkpoint,DeletionHistoryCheckpoint)
            or type(maximum_database_bytes) is not int or not 65536<=maximum_database_bytes<=128<<30
            or not callable(check_active)):
        raise ValueError('DELETION_BACKUP_RESTORE_INSTALLATION')
    expected_files={'manifest.json','history.sqlite'}
    if set(os.listdir(root))!=expected_files:
        raise ValueError('DELETION_BACKUP_CLOSURE')
    check_active();manifest_path=root/'manifest.json';database_path=root/'history.sqlite'
    pin,_=_file_digest(manifest_path,4096,check_active)
    if pin!=manifest_sha:raise ValueError('DELETION_BACKUP_MANIFEST_PIN')
    descriptor=os.open(manifest_path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    try:raw=os.read(descriptor,4097)
    finally:os.close(descriptor)
    if len(raw)>4096:raise ValueError('DELETION_BACKUP_LIMIT')
    if sha256(raw).hexdigest()!=manifest_sha:raise ValueError('DELETION_BACKUP_CHANGED')
    manifest=parse_json(raw,max_bytes=4096)
    if (not isinstance(manifest,dict) or set(manifest)!={'schema_version','backup_id','database_sha256','database_bytes','sequence','head_sha256'}
            or manifest['schema_version']!='deletion-history-backup/v1' or canonical_bytes(manifest)!=raw
            or manifest['backup_id']!=root.name or not _sha(manifest['database_sha256'])
            or type(manifest['database_bytes']) is not int or type(manifest['sequence']) is not int
            or manifest['sequence']!=latest_checkpoint.sequence or manifest['head_sha256']!=latest_checkpoint.head_sha256):
        raise ValueError('DELETION_BACKUP_STALE_OR_INVALID')
    digest,size=_file_digest(database_path,maximum_database_bytes,check_active)
    if digest!=manifest['database_sha256'] or size!=manifest['database_bytes']:
        raise ValueError('DELETION_BACKUP_DATABASE_PIN')
    database=sqlite3.connect('file:'+quote(str(database_path),safe='/')+'?mode=ro&immutable=1',uri=True)
    try:
        database.execute('PRAGMA query_only=ON');database.execute('PRAGMA cache_size=-2048')
        cursor=database.execute('SELECT body,sha256 FROM events ORDER BY sequence')
        def rows():
            for body,row_sha in cursor:
                check_active()
                if sha256(body).hexdigest()!=row_sha:raise ValueError('DELETION_BACKUP_CORRUPT')
                entry=parse_json(body,max_bytes=4096)
                if canonical_bytes(entry)!=body:raise ValueError('DELETION_BACKUP_CORRUPT')
                yield entry
            check_active()
        iterator=rows()
        try:yield latest_checkpoint,iterator
        finally:iterator.close();cursor.close()
    finally:database.close()
    # A changed file cannot rescue an earlier partial/incorrect SQL observation.
    if _file_digest(database_path,maximum_database_bytes,check_active)!=(digest,size):
        raise ValueError('DELETION_BACKUP_CHANGED')
    if _file_digest(manifest_path,4096,check_active)[0]!=manifest_sha:
        raise ValueError('DELETION_BACKUP_CHANGED')
    if set(os.listdir(root))!=expected_files:
        raise ValueError('DELETION_BACKUP_CLOSURE')


def verify_restore_deletion_backup(connection,directory,manifest_sha,latest_checkpoint,*,
                                    expected_database,check_active,maximum_database_bytes=1<<30):
    with open_deletion_history_backup(directory,manifest_sha,latest_checkpoint,
            check_active=check_active,maximum_database_bytes=maximum_database_bytes) as (head,entries):
        evidence=verify_restore_database_deletions(connection,entries,head,
            expected_database=expected_database,check_active=check_active)
    return evidence

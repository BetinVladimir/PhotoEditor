"""Bounded disk-sorted all-version catalogue for isolated MinIO restore.

Object-store writers must be paused by the restore orchestrator. Listing is not
an atomic storage snapshot. Temporary SQLite contains only public object metadata
and is removed on completion, cancellation or failure; it is not restore evidence.
"""
from contextlib import contextmanager
from pathlib import Path
import sqlite3
import tempfile
from .restore_inventory import RestoreObject, _validate
from .restore_quarantine import RestorePhysicalVersion
from .restore_authority import require_restore_active


@contextmanager
def sorted_restore_versions(client, buckets, scratch_root, *, check_active,
                            maximum_entries=10_000_000, maximum_spool_bytes=1<<30):
    if (not callable(getattr(client,'list_objects',None)) or not callable(check_active)
            or not isinstance(buckets,(tuple,list)) or not 1<=len(buckets)<=256
            or any(not isinstance(b,str) or not 3<=len(b)<=63 for b in buckets)
            or len(set(buckets))!=len(buckets)
            or type(maximum_entries) is not int or not 1<=maximum_entries<=10_000_000
            or type(maximum_spool_bytes) is not int or not 65536<=maximum_spool_bytes<=128<<30):
        raise ValueError('RESTORE_LIST_INSTALLATION')
    root=Path(scratch_root)
    require_restore_active(check_active)
    if not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir():
        raise ValueError('RESTORE_LIST_SCRATCH')
    installed=tuple(buckets)
    with tempfile.TemporaryDirectory(prefix='restore-catalogue-',dir=root) as directory:
        database=sqlite3.connect(str(Path(directory)/'versions.sqlite'))
        try:
            database.execute('PRAGMA journal_mode=OFF')
            database.execute('PRAGMA synchronous=OFF')
            database.execute('PRAGMA temp_store=FILE')
            database.execute('PRAGMA cache_size=-2048')
            database.execute('CREATE TABLE versions(bucket BLOB,key BLOB,version BLOB,size INTEGER NOT NULL,PRIMARY KEY(bucket,key,version)) WITHOUT ROWID')
            count=0
            for bucket in installed:
                require_restore_active(check_active)
                listing=iter(client.list_objects(bucket,recursive=True,include_version=True))
                try:
                    for item in listing:
                        require_restore_active(check_active);count+=1
                        if count>maximum_entries:raise ValueError('RESTORE_LIST_LIMIT')
                        marker=getattr(item,'is_delete_marker',False)
                        if type(marker) is not bool:raise ValueError('RESTORE_LIST_INVALID')
                        if marker:continue
                        ref=RestoreObject(bucket,item.object_name,item.version_id,'0'*64,item.size)
                        _validate(ref)
                        try:
                            database.execute('INSERT INTO versions VALUES(?,?,?,?)',
                                (bucket.encode(),ref.key.encode(),ref.version.encode(),ref.size))
                        except sqlite3.IntegrityError:
                            raise ValueError('RESTORE_LIST_DUPLICATE_VERSION') from None
                        pages=database.execute('PRAGMA page_count').fetchone()[0]
                        page_size=database.execute('PRAGMA page_size').fetchone()[0]
                        if pages*page_size>maximum_spool_bytes:raise ValueError('RESTORE_LIST_SPOOL_LIMIT')
                finally:
                    close=getattr(listing,'close',None)
                    if callable(close):close()
            database.commit();require_restore_active(check_active)
            cursor=database.execute('SELECT bucket,key,version,size FROM versions ORDER BY bucket,key,version')
            def rows():
                for bucket,key,version,size in cursor:
                    require_restore_active(check_active)
                    yield RestorePhysicalVersion(bucket.decode(),key.decode(),version.decode(),size)
                require_restore_active(check_active)
            iterator=rows()
            try:yield iterator
            finally:iterator.close();cursor.close()
        finally:database.close()

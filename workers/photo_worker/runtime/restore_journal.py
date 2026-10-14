"""Durable operator quarantine-plan journal, never an object mutation authority.

Only a final <run UUID> directory with complete.json is a sealed report. A
<run UUID>.incomplete directory survives failures for inspection and must never
be activated. This journal does not establish deletion-history reconciliation.
"""
from dataclasses import asdict
from hashlib import sha256
import os
from pathlib import Path
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes
from .restore_inventory import RestoreObject, _validate
from .restore_quarantine import RestorePhysicalVersion
from .restore_catalogue import RestoreStorageEvidence
from .restore_authority import require_restore_active
from .atomic_directory import (directory_identity as _directory_identity,
                               rename_exclusive as _rename_exclusive,
                               sync_directory as _sync_directory)


class RestoreQuarantineJournal:
    def __init__(self,root,run_id,*,check_active,maximum_bytes=1<<30):
        path=Path(root)
        if (not path.is_absolute() or path.resolve(strict=True)!=path or not path.is_dir() or path.stat().st_mode & 0o022
                or not isinstance(run_id,UUID) or run_id.int==0 or not callable(check_active)
                or type(maximum_bytes) is not int or not 1<=maximum_bytes<=128<<30):
            raise ValueError('RESTORE_JOURNAL_INSTALLATION')
        require_restore_active(check_active)
        self.root=path;self.final=path/str(run_id);self.pending=path/(str(run_id)+'.incomplete')
        self.root_identity=_directory_identity(path)
        if self.final.exists():raise ValueError('RESTORE_JOURNAL_EXISTS')
        os.mkdir(self.pending,0o700)
        try:
            descriptor=os.open(self.pending/'extras.jsonl',os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
            self.file=os.fdopen(descriptor,'wb')
        except BaseException:
            _sync_directory(path)
            raise
        self.active=check_active;self.maximum=maximum_bytes;self.written=0
        self.count=self.extra_bytes=0;self.previous=None
        self.digest=sha256(b'photoeditor.restore-quarantine-plan/v1\n')
        self.closed=self.sealed=self.failed=False
        try:_sync_directory(path)
        except BaseException:
            self.close()
            raise

    def __enter__(self):return self
    def __exit__(self,*_):self.close()
    def close(self):
        if not self.closed:self.closed=True;self.file.close()

    def stage(self,version):
        try:self._stage(version)
        except BaseException:
            self.failed=True
            raise

    def _stage(self,version):
        require_restore_active(self.active)
        if self.closed or self.sealed or self.failed or not isinstance(version,RestorePhysicalVersion):
            raise ValueError('RESTORE_JOURNAL_CLOSED')
        _validate(RestoreObject(version.bucket,version.key,version.version,'0'*64,version.size))
        identity=tuple(v.encode() for v in (version.bucket,version.key,version.version))
        if self.previous is not None and identity<=self.previous:raise ValueError('RESTORE_JOURNAL_ORDER')
        raw=canonical_bytes(asdict(version))+b'\n'
        if len(raw)>self.maximum-self.written:raise ValueError('RESTORE_JOURNAL_LIMIT')
        if self.file.write(raw)!=len(raw):raise OSError('RESTORE_JOURNAL_SHORT_WRITE')
        self.digest.update(raw)
        self.written+=len(raw);self.count+=1;self.extra_bytes+=version.size;self.previous=identity
        require_restore_active(self.active)

    def seal(self,evidence):
        try:return self._seal(evidence)
        except BaseException:
            self.failed=True
            raise

    def _seal(self,evidence):
        require_restore_active(self.active)
        if self.closed or self.sealed or self.failed or not isinstance(evidence,RestoreStorageEvidence):
            raise ValueError('RESTORE_JOURNAL_EVIDENCE_INVALID')
        if _directory_identity(self.root)!=self.root_identity:
            raise ValueError('RESTORE_JOURNAL_ROOT_CHANGED')
        q=evidence.quarantine
        if (q.plan_sha256!=self.digest.hexdigest() or q.extra!=self.count
                or q.extra_bytes!=self.extra_bytes or q.referenced!=evidence.database.inventory.objects
                or q.physical!=q.referenced+q.extra):
            raise ValueError('RESTORE_JOURNAL_EVIDENCE_MISMATCH')
        document=asdict(evidence)
        document['database']['observed_at']=evidence.database.observed_at.isoformat()
        raw=canonical_bytes({'schema_version':'restore-quarantine-journal/v1',
            'run_id':self.final.name,'storage':document,'extras_bytes':self.written})
        self.file.flush();os.fsync(self.file.fileno())
        descriptor=os.open(self.pending/'complete.json',os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
        with os.fdopen(descriptor,'wb') as marker:
            if marker.write(raw)!=len(raw):raise OSError('RESTORE_JOURNAL_SHORT_WRITE')
            marker.flush();os.fsync(marker.fileno())
        _sync_directory(self.pending);require_restore_active(self.active)
        # Existing run directories must never be replaced, including empty ones.
        # Unique operator UUIDs plus this final check enforce one journal per run.
        if self.final.exists():raise ValueError('RESTORE_JOURNAL_EXISTS')
        root_fd=os.open(self.root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
        try:
            root_info=os.fstat(root_fd)
            if ((root_info.st_dev,root_info.st_ino)!=self.root_identity
                    or _directory_identity(self.root)!=self.root_identity):
                raise ValueError('RESTORE_JOURNAL_ROOT_CHANGED')
            _rename_exclusive(root_fd,self.pending.name,self.final.name)
            os.fsync(root_fd)
            if _directory_identity(self.root)!=self.root_identity:
                raise ValueError('RESTORE_JOURNAL_ROOT_CHANGED')
        finally:os.close(root_fd)
        self.sealed=True;self.close()
        return self.final

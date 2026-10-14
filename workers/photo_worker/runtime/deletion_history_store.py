"""Independent operator deletion-history store; never place in restored DB.

The operator owns this directory and its backup/replication. SQLite transaction
serialization and FULL synchronization protect append consistency; filesystem
administrators remain trusted. API deletion integration must durably publish a
requested event before destructive cleanup. This module alone does not wire it.
"""
from contextlib import contextmanager
from datetime import datetime,timezone
from hashlib import sha256
import os
from pathlib import Path
import sqlite3
import stat
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_deletions import DeletionHistoryCheckpoint


class DeletionHistoryStore:
    def __init__(self,root,*,check_active,root_fd=None):
        if not callable(check_active):
            raise ValueError('DELETION_HISTORY_INSTALLATION')
        if root_fd is None:
            path=Path(root)
            if (not path.is_absolute() or path.resolve(strict=True)!=path or not path.is_dir()
                    or path.stat().st_mode & 0o022):
                raise ValueError('DELETION_HISTORY_INSTALLATION')
            self.path=path/'deletion-history.sqlite'
        else:
            if root is not None or type(root_fd) is not int or root_fd < 3:
                raise ValueError('DELETION_HISTORY_INSTALLATION')
            info=os.fstat(root_fd)
            if (not stat.S_ISDIR(info.st_mode) or info.st_mode & 0o077
                    or info.st_uid not in (0, os.geteuid())):
                raise ValueError('DELETION_HISTORY_INSTALLATION')
            # The CLI is a single-purpose process. Its cwd pins the directory
            # inode for every relative SQLite open even if its old path moves.
            os.fchdir(root_fd)
            self.path=Path('deletion-history.sqlite')
        self.active=check_active
        check_active()
        # Reserve a regular, private single-link file before SQLite opens it.
        descriptor=os.open(self.path,os.O_RDWR|os.O_CREAT|os.O_NOFOLLOW|os.O_NONBLOCK,0o600)
        try:
            info=os.fstat(descriptor)
            if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or info.st_mode & 0o077:
                raise ValueError('DELETION_HISTORY_FILE')
        finally:os.close(descriptor)
        database=self._connect()
        try:
            database.execute('CREATE TABLE IF NOT EXISTS events(sequence INTEGER PRIMARY KEY,event_id TEXT UNIQUE NOT NULL,body BLOB NOT NULL,sha256 TEXT NOT NULL)')
            database.execute("CREATE TRIGGER IF NOT EXISTS events_no_update BEFORE UPDATE ON events BEGIN SELECT RAISE(ABORT,'append only'); END")
            database.execute("CREATE TRIGGER IF NOT EXISTS events_no_delete BEFORE DELETE ON events BEGIN SELECT RAISE(ABORT,'append only'); END")

        finally:database.close()

    def _connect(self):
        database=sqlite3.connect(self.path,timeout=5,isolation_level=None)
        database.execute('PRAGMA journal_mode=WAL');database.execute('PRAGMA synchronous=FULL')
        database.execute('PRAGMA cache_size=-2048')
        return database

    def append(self,event_id,tenant_id,phase,occurred_at):
        self.active()
        if (not isinstance(event_id,UUID) or event_id.int==0 or not isinstance(tenant_id,UUID)
                or tenant_id.int==0 or phase not in ('requested','completed')
                or not isinstance(occurred_at,datetime) or occurred_at.tzinfo is None):
            raise ValueError('DELETION_HISTORY_EVENT')
        timestamp=occurred_at.astimezone(timezone.utc).isoformat()
        database=self._connect()
        try:
            database.execute('BEGIN IMMEDIATE')
            old=database.execute('SELECT sequence,body,sha256 FROM events WHERE event_id=?',(str(event_id),)).fetchone()
            if old is not None:
                entry=parse_json(old[1],max_bytes=4096)
                if (entry['tenant_id']!=str(tenant_id) or entry['phase']!=phase
                        or entry['occurred_at']!=timestamp or sha256(old[1]).hexdigest()!=old[2]):
                    raise ValueError('DELETION_HISTORY_IDEMPOTENCY_CONFLICT')
                self.active();database.commit()
                return DeletionHistoryCheckpoint(old[0],old[2])
            last=database.execute('SELECT sequence,sha256 FROM events ORDER BY sequence DESC LIMIT 1').fetchone()
            sequence,previous=(1,'0'*64) if last is None else (last[0]+1,last[1])
            if sequence>2**53-1:raise ValueError('DELETION_HISTORY_EXHAUSTED')
            body=canonical_bytes({'schema_version':'tenant-deletion-history/v1','sequence':sequence,
                'previous_sha256':previous,'tenant_id':str(tenant_id),'phase':phase,'occurred_at':timestamp})
            digest=sha256(body).hexdigest()
            database.execute('INSERT INTO events VALUES(?,?,?,?)',(sequence,str(event_id),body,digest))
            self.active();database.commit()
            return DeletionHistoryCheckpoint(sequence,digest)
        except BaseException:
            database.rollback();raise
        finally:database.close()

    @contextmanager
    def snapshot(self):
        """Trusted latest head and full stream from the same independent snapshot."""
        self.active();database=self._connect()
        try:
            database.execute('PRAGMA query_only=ON');database.execute('BEGIN')
            last=database.execute('SELECT sequence,sha256 FROM events ORDER BY sequence DESC LIMIT 1').fetchone()
            checkpoint=DeletionHistoryCheckpoint(0,'0'*64) if last is None else DeletionHistoryCheckpoint(*last)
            cursor=database.execute('SELECT body,sha256 FROM events ORDER BY sequence')
            def entries():
                for body,digest in cursor:
                    self.active()
                    if sha256(body).hexdigest()!=digest:raise ValueError('DELETION_HISTORY_CORRUPT')
                    entry=parse_json(body,max_bytes=4096)
                    if canonical_bytes(entry)!=body:raise ValueError('DELETION_HISTORY_CORRUPT')
                    yield entry
                self.active()
            iterator=entries()
            try:yield checkpoint,iterator
            finally:iterator.close();cursor.close()
        finally:database.rollback();database.close()

    def latest_verified_checkpoint(self, *, maximum_entries=10_000_000):
        """Read current independent authority, validating its entire closed chain.

        This must be the live operator store, outside the restored namespace;
        opening a portable backup as this authority cannot establish freshness.
        No restored tenant lookup is performed here: preflight does that against
        the separately pinned backup using this independently obtained head.
        """
        from .restore_deletions import verify_restore_deletions
        self.active()
        with self.snapshot() as (checkpoint, entries):
            verify_restore_deletions(entries, checkpoint, lambda tenant: None,
                check_active=self.active, maximum_entries=maximum_entries)
        self.active()
        return checkpoint

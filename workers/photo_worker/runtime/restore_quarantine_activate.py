"""Atomically activate sealed extras as SQL-enforced physical-version quarantine."""
from dataclasses import asdict,dataclass
from hashlib import sha256
import os
import stat
import psycopg
from photo_worker.contracts.base import canonical_bytes,parse_json
from .restore_preflight import RestorePreflightEvidence
from .restore_key_installation import _read_private,_open_private_descriptor
from .restore_inventory import RestoreObject,_validate


@dataclass(frozen=True)
class RestoreQuarantineActivation:
    schema_version: str
    run_id: str
    plan_sha256: str
    versions: int
    bytes: int


def activate_restore_quarantine(connection,preflight,*,expected_database,
                                assert_isolated_and_paused,check_active):
    if (not isinstance(preflight,RestorePreflightEvidence) or not callable(check_active)
            or not callable(assert_isolated_and_paused)
            or preflight.storage.database.database!=expected_database):
        raise ValueError('RESTORE_QUARANTINE_ACTIVATION_INSTALLATION')
    def active():
        if check_active() is False:
            raise RuntimeError('RESTORE_QUARANTINE_INACTIVE')
    def guard():
        active()
        if assert_isolated_and_paused() is False:
            raise RuntimeError('RESTORE_WRITERS_NOT_PAUSED')
        active()
    guard();root=preflight.quarantine_journal
    if not root.is_absolute() or root.resolve(strict=True)!=root or root.name!=str(preflight.run_id):
        raise ValueError('RESTORE_QUARANTINE_JOURNAL_PATH')
    marker_raw=_read_private(root/'complete.json',1<<20,active)
    marker=parse_json(marker_raw,max_bytes=1<<20)
    storage=asdict(preflight.storage)
    storage['database']['observed_at']=preflight.storage.database.observed_at.isoformat()
    if (not isinstance(marker,dict) or set(marker)!= {'schema_version','run_id','storage','extras_bytes'}
            or marker['schema_version']!='restore-quarantine-journal/v1'
            or marker['run_id']!=str(preflight.run_id) or marker['storage']!=storage
            or canonical_bytes(marker)!=marker_raw or type(marker['extras_bytes']) is not int
            or not 0<=marker['extras_bytes']<=1<<30):
        raise ValueError('RESTORE_QUARANTINE_MARKER')
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    path=root/'extras.jsonl';descriptor=_open_private_descriptor(path,guard)
    with os.fdopen(descriptor,'rb') as stream:
        before=os.fstat(stream.fileno())
        if (not stat.S_ISREG(before.st_mode) or before.st_nlink!=1 or before.st_mode & 0o077
                or before.st_uid!=os.getuid() or before.st_size!=marker['extras_bytes']):
            raise ValueError('RESTORE_QUARANTINE_STREAM_FILE')
        q=preflight.storage.quarantine;count=total=received=0;previous=None
        digest=sha256(b'photoeditor.restore-quarantine-plan/v1\n')
        with connection.transaction():
            connection.execute('SET TRANSACTION ISOLATION LEVEL READ COMMITTED')
            connection.execute("SET LOCAL statement_timeout = '30s'")
            connection.execute("SET LOCAL lock_timeout = '5s'")
            connection.execute('SET LOCAL row_security = off')
            row=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
            if row!=(expected_database,True):raise ValueError('RESTORE_QUARANTINE_DATABASE_AUTHORITY')
            values=(preflight.run_id,q.plan_sha256,q.extra,q.extra_bytes)
            connection.execute('INSERT INTO photo.restore_quarantine_runs(id,plan_sha256,extra_count,extra_bytes) VALUES(%s,%s,%s,%s) ON CONFLICT DO NOTHING',values)
            receipt=connection.execute('SELECT plan_sha256,extra_count,extra_bytes FROM photo.restore_quarantine_runs WHERE id=%s',(preflight.run_id,)).fetchone()
            if receipt!=values[1:]:raise ValueError('RESTORE_QUARANTINE_RUN_CONFLICT')
            while True:
                guard();raw=stream.readline(8193)
                if not raw:break
                received+=len(raw)
                if len(raw)>8192 or not raw.endswith(b'\n') or received>before.st_size or count>=q.extra:
                    raise ValueError('RESTORE_QUARANTINE_STREAM_LIMIT')
                value=parse_json(raw[:-1],max_bytes=8192)
                if (not isinstance(value,dict) or set(value)!= {'bucket','key','version','size'}
                        or canonical_bytes(value)+b'\n'!=raw):raise ValueError('RESTORE_QUARANTINE_STREAM_ROW')
                _validate(RestoreObject(value['bucket'],value['key'],value['version'],'0'*64,value['size']))
                identity=tuple(value[k].encode() for k in ('bucket','key','version'))
                if previous is not None and identity<=previous:raise ValueError('RESTORE_QUARANTINE_STREAM_ORDER')
                physical=(value['bucket'],value['key'],value['version'])
                connection.execute('SELECT pg_catalog.pg_advisory_xact_lock(pg_catalog.hashtextextended(pg_catalog.jsonb_build_array(%s,%s,%s)::text,0))',physical)
                exists=connection.execute("SELECT EXISTS(SELECT FROM photo.artifacts WHERE state='ready' AND bucket=%s AND object_key=%s AND object_version=%s)",physical).fetchone()
                if exists!=(False,):raise ValueError('RESTORE_QUARANTINE_REFERENCED_OBJECT')
                connection.execute('INSERT INTO photo.restore_quarantine_versions(run_id,bucket,object_key,object_version,bytes) VALUES(%s,%s,%s,%s,%s) ON CONFLICT DO NOTHING',(preflight.run_id,*physical,value['size']))
                stored=connection.execute('SELECT bytes FROM photo.restore_quarantine_versions WHERE run_id=%s AND bucket=%s AND object_key=%s AND object_version=%s',(preflight.run_id,*physical)).fetchone()
                if stored!=(value['size'],):raise ValueError('RESTORE_QUARANTINE_VERSION_CONFLICT')
                digest.update(raw);count+=1;total+=value['size'];previous=identity
            if count!=q.extra or total!=q.extra_bytes or digest.hexdigest()!=q.plan_sha256 or received!=before.st_size:
                raise ValueError('RESTORE_QUARANTINE_STREAM_MISMATCH')
            identity=lambda s:(s.st_dev,s.st_ino,s.st_size,s.st_mode,s.st_nlink,s.st_mtime_ns,s.st_ctime_ns)
            if (identity(before)!=identity(os.fstat(stream.fileno()))
                    or identity(before)!=identity(path.lstat())
                    or root.resolve(strict=True)!=root):
                raise ValueError('RESTORE_QUARANTINE_STREAM_CHANGED')
            if _read_private(root/'complete.json',1<<20,active)!=marker_raw:
                raise ValueError('RESTORE_QUARANTINE_MARKER_CHANGED')
            actual=connection.execute('SELECT count(*),COALESCE(sum(bytes),0)::bigint FROM photo.restore_quarantine_versions WHERE run_id=%s',(preflight.run_id,)).fetchone()
            if actual!=(count,total):raise ValueError('RESTORE_QUARANTINE_ACTIVATION_RECEIPT')
            guard()
    guard()
    return RestoreQuarantineActivation('restore-quarantine-activation/v1',str(preflight.run_id),q.plan_sha256,count,total)

"""Read actual pinned recovery/replay files before restore acceptance probes."""
from dataclasses import asdict, dataclass
from datetime import datetime
from hashlib import sha256
import re
from pathlib import Path
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes, parse_json
from .restore_key_installation import _read_private
from .restore_recovery import RestoreComputeRecoveryEvidence
from .restore_report import _json


@dataclass(frozen=True)
class VerifiedRestoreReplay:
    run_id: UUID
    database: str
    build_sha256: str
    recovery_sha256: str
    replay_sha256: str
    confirmed: int
    completed_at: datetime


def verify_restore_replay_files(recovery_path, recovery_sha256, replay_path, replay_sha256,
                                *, evidence, build_sha256, check_active):
    """Pins are independent operator inputs, not extracted from either file.

    No writer approval. Re-read both immutable files after parsing/binding.
    Probe coordinator must keep independent isolation/phase authority installed.
    """
    if (not isinstance(evidence, RestoreComputeRecoveryEvidence) or not callable(check_active)
            or any(not isinstance(pin,str) or re.fullmatch('[0-9a-f]{64}',pin) is None
                   for pin in (recovery_sha256,replay_sha256,build_sha256))):
        raise ValueError('RESTORE_REPLAY_INSTALLATION')
    run_id=evidence.preflight.run_id
    if (not isinstance(run_id,UUID) or run_id.int==0
            or Path(recovery_path).name!=str(run_id)+'.json'
            or Path(replay_path).name!=str(run_id)+'.replay.json'):
        raise ValueError('RESTORE_REPLAY_FILE_IDENTITY')
    def load(path,pin,limit):
        raw=_read_private(path,limit,check_active)
        if sha256(raw).hexdigest()!=pin:raise ValueError('RESTORE_REPLAY_PIN')
        value=parse_json(raw,max_bytes=limit)
        if canonical_bytes(value)!=raw:raise ValueError('RESTORE_REPLAY_CANONICAL')
        return value
    recovery=load(recovery_path,recovery_sha256,8<<20)
    replay=load(replay_path,replay_sha256,16384)
    recovery_keys={'schema_version','writers_authorized','build_sha256','backup_at',
                   'last_wal_lsn','started_at','finished_at','recovery'}
    replay_keys={'schema_version','run_id','recovery_sha256','build_sha256','database',
                 'initially_pending','confirmed','completed_at','writers_authorized'}
    if (not isinstance(recovery,dict) or set(recovery)!=recovery_keys
            or not isinstance(replay,dict) or set(replay)!=replay_keys
            or recovery['schema_version']!='restore-recovery-report/v1'
            or replay['schema_version']!='restore-task-replay-receipt/v1'
            or recovery['writers_authorized'] is not False or replay['writers_authorized'] is not False
            or recovery['build_sha256']!=build_sha256 or replay['build_sha256']!=build_sha256
            or replay['recovery_sha256']!=recovery_sha256
            or recovery['recovery']!=_json(asdict(evidence))
            or replay['run_id']!=str(evidence.preflight.run_id)
            or replay['database']!=evidence.outbox.database
            or replay['database']!=evidence.preflight.storage.database.database):
        raise ValueError('RESTORE_REPLAY_BINDING')
    pending=evidence.outbox.pending_task_events
    if (type(pending) is not int or not 0<=pending<=10_000_000
            or any(type(replay[key]) is not int or replay[key]!=pending
                   for key in ('initially_pending','confirmed'))):
        raise ValueError('RESTORE_REPLAY_COUNTS')
    def timestamp(value):
        if not isinstance(value,str):raise ValueError('RESTORE_REPLAY_TIME')
        result=datetime.fromisoformat(value.replace('Z','+00:00'))
        if result.tzinfo is None:raise ValueError('RESTORE_REPLAY_TIME')
        return result
    backup,start,finish,completed=(timestamp(value) for value in (
        recovery['backup_at'],recovery['started_at'],recovery['finished_at'],replay['completed_at']))
    if not backup<=start<=finish<=completed:raise ValueError('RESTORE_REPLAY_TIME')
    if not isinstance(recovery['last_wal_lsn'],str) or re.fullmatch('[0-9A-F]{1,8}/[0-9A-F]{1,8}',recovery['last_wal_lsn']) is None:
        raise ValueError('RESTORE_REPLAY_WAL')
    load(recovery_path,recovery_sha256,8<<20);load(replay_path,replay_sha256,16384)
    if check_active() is False:
        raise RuntimeError('RESTORE_REPLAY_INACTIVE')
    return VerifiedRestoreReplay(evidence.preflight.run_id,replay['database'],build_sha256,
                                 recovery_sha256,replay_sha256,pending,completed)

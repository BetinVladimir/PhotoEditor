"""Durable operator recovery evidence, explicitly not writer approval."""
from dataclasses import asdict,dataclass
from datetime import datetime,timezone
from hashlib import sha256
import math
import os
import stat
from pathlib import Path
import re
from uuid import UUID
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from .restore_recovery import RestoreComputeRecoveryEvidence
from .restore_quarantine_activate import RestoreQuarantineActivation


@dataclass(frozen=True)
class RestoreReportReceipt:
    path: Path
    sha256: str


def _json(value):
    if isinstance(value,ArtifactRef):
        return {'artifact_id':str(value.artifact_id),'sha256':value.sha256}
    if isinstance(value,datetime):
        if value.tzinfo is None:raise ValueError('RESTORE_REPORT_TIME')
        return value.astimezone(timezone.utc).isoformat()
    if isinstance(value,(UUID,Path)):return str(value)
    if isinstance(value,dict):return {key:_json(item) for key,item in value.items()}
    if isinstance(value,(list,tuple)):return [_json(item) for item in value]
    if type(value) is float and math.isfinite(value):return value
    if value is None or type(value) in (str,int,bool):return value
    raise ValueError('RESTORE_REPORT_VALUE')


def _open_report_directory(root, active):
    """Pin every ancestor, so a replaced symlink cannot redirect publication."""
    descriptor=os.open('/',os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        for component in root.parts[1:]:
            active()
            child=os.open(component,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW,
                          dir_fd=descriptor)
            os.close(descriptor)
            descriptor=child
        active()
        result,descriptor=descriptor,None
        return result
    except OSError as error:
        raise ValueError('RESTORE_REPORT_ROOT_CHANGED') from error
    finally:
        if descriptor is not None:os.close(descriptor)


def persist_recovery_report(root,evidence,*,build_sha256,backup_at,last_wal_lsn,
                            started_at,finished_at,check_active):
    """Publish exclusive canonical JSON only after file fsync; never overwrite.

    Report is stage evidence. Keys signing/KMS, quarantine activation, broker
    replay, probes and operator approval must still complete before writers.
    A failure after publication may leave a report; it never implies approval.
    """
    root=Path(root)
    if (not isinstance(evidence,RestoreComputeRecoveryEvidence)
            or not isinstance(evidence.preflight.run_id,UUID) or evidence.preflight.run_id.int==0
            or not isinstance(build_sha256,str) or re.fullmatch('[0-9a-f]{64}',build_sha256) is None
            or not isinstance(last_wal_lsn,str) or re.fullmatch('[0-9A-F]{1,8}/[0-9A-F]{1,8}',last_wal_lsn) is None
            or not callable(check_active)):
        raise ValueError('RESTORE_REPORT_INSTALLATION')
    def active():
        if check_active() is False:
            raise RuntimeError('RESTORE_REPORT_INACTIVE')
    active()
    if (not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir()
            or root.stat().st_uid!=os.getuid() or root.stat().st_mode & 0o022):
        raise ValueError('RESTORE_REPORT_INSTALLATION')
    if (any(not isinstance(t,datetime) or t.tzinfo is None for t in (backup_at,started_at,finished_at))
            or not backup_at<=started_at<=finished_at):raise ValueError('RESTORE_REPORT_TIME')
    run_id=str(evidence.preflight.run_id)
    quarantine=evidence.quarantine
    plan=evidence.preflight.storage.quarantine
    if (evidence.schema_version!='restore-compute-recovery/v1'
            or evidence.preflight.schema_version!='restore-preflight/v1'
            or not isinstance(quarantine,RestoreQuarantineActivation)
            or quarantine.schema_version!='restore-quarantine-activation/v1'
            or quarantine.run_id!=run_id
            or quarantine.plan_sha256!=plan.plan_sha256
            or quarantine.versions!=plan.extra
            or quarantine.bytes!=plan.extra_bytes):
        raise ValueError('RESTORE_REPORT_EVIDENCE_IDENTITY')
    document={'schema_version':'restore-recovery-report/v1','writers_authorized':False,
        'build_sha256':build_sha256,'backup_at':backup_at,'last_wal_lsn':last_wal_lsn,
        'started_at':started_at,'finished_at':finished_at,'recovery':asdict(evidence)}
    raw=canonical_bytes(_json(document))
    if len(raw)>8<<20:raise ValueError('RESTORE_REPORT_LIMIT')
    stem=str(evidence.preflight.run_id);pending=stem+'.pending';final=stem+'.json'
    directory=_open_report_directory(root,active)
    try:
        before=os.fstat(directory)
        if (not stat.S_ISDIR(before.st_mode) or before.st_uid!=os.getuid()
                or before.st_mode & 0o022):
            raise ValueError('RESTORE_REPORT_INSTALLATION')
        identity=(before.st_dev,before.st_ino)
        def check_root():
            current=os.stat(root,follow_symlinks=False)
            if (not stat.S_ISDIR(current.st_mode)
                    or (current.st_dev,current.st_ino)!=identity):
                raise ValueError('RESTORE_REPORT_ROOT_CHANGED')
        check_root()
        descriptor=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,
                           0o600,dir_fd=directory)
        try:
            with os.fdopen(descriptor,'wb') as stream:
                if stream.write(raw)!=len(raw):raise OSError('RESTORE_REPORT_SHORT_WRITE')
                stream.flush();os.fsync(stream.fileno())
            active();check_root()
            # A hard link is an atomic no-replace publication in the pinned directory.
            os.link(pending,final,src_dir_fd=directory,dst_dir_fd=directory,
                    follow_symlinks=False)
            os.unlink(pending,dir_fd=directory)
            os.fsync(directory)
            check_root();active();check_root()
            return RestoreReportReceipt(root/final,sha256(raw).hexdigest())
        finally:
            try:os.unlink(pending,dir_fd=directory)
            except FileNotFoundError:pass
    finally:
        os.close(directory)

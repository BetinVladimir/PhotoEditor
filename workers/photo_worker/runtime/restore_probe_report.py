"""Private exclusive probe artifacts with bound concrete observations."""
from datetime import datetime
from hashlib import sha256
import os
from pathlib import Path
import re
import stat
from photo_worker.contracts.base import canonical_bytes
from .restore_acceptance import (PROBE_CHECKS,RestoreProbeReceipt,read_restore_probe_report,
    validate_restore_probe_observations,_require_restore_guard)
from .restore_replay_receipt import VerifiedRestoreReplay
from .restore_report import _json


def validate_probe_report_root(root):
    root=Path(root)
    if (not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir()
            or root.stat().st_uid!=os.getuid() or root.stat().st_mode & 0o022):
        raise ValueError('RESTORE_PROBE_REPORT_ROOT')
    return root


def persist_restore_probe_report(root,replay,kind,fixture_sha256,started_at,finished_at,observations,*,check_active):
    if not callable(check_active):raise ValueError('RESTORE_PROBE_REPORT_INSTALLATION')
    def guard():_require_restore_guard(check_active,'RESTORE_PROBE_REPORT_INACTIVE')
    guard()
    root=validate_probe_report_root(root);kinds=dict(PROBE_CHECKS)
    if (not isinstance(replay,VerifiedRestoreReplay) or kind not in kinds
            or not isinstance(fixture_sha256,str) or re.fullmatch('[0-9a-f]{64}',fixture_sha256) is None
            or not isinstance(started_at,datetime) or started_at.tzinfo is None
            or not isinstance(finished_at,datetime) or finished_at.tzinfo is None
            or not replay.completed_at<=started_at<=finished_at
            or not isinstance(observations,dict) or not observations or not callable(check_active)):
        raise ValueError('RESTORE_PROBE_REPORT_INSTALLATION')
    document={'schema_version':'restore-probe-report/v1','kind':kind,'run_id':str(replay.run_id),
        'database':replay.database,'build_sha256':replay.build_sha256,'fixture_sha256':fixture_sha256,
        'recovery_sha256':replay.recovery_sha256,'replay_sha256':replay.replay_sha256,
        'started_at':started_at,'finished_at':finished_at,'checks':[[name,True] for name in kinds[kind]],
        'observations':observations}
    normalized=_json(document)
    raw=canonical_bytes(normalized)
    if len(raw)>262144:raise ValueError('RESTORE_PROBE_REPORT_LIMIT')
    validate_restore_probe_observations(kind,normalized['observations'],replay,fixture_sha256)
    stem=str(replay.run_id)+'.'+kind;pending=stem+'.pending';final=stem+'.json'
    guard();directory=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        before=os.fstat(directory)
        if (not stat.S_ISDIR(before.st_mode) or before.st_uid!=os.getuid()
                or before.st_mode & 0o022):
            raise ValueError('RESTORE_PROBE_REPORT_ROOT')
        identity=(before.st_dev,before.st_ino)
        def check_root():
            current=os.stat(root,follow_symlinks=False)
            if (not stat.S_ISDIR(current.st_mode)
                    or (current.st_dev,current.st_ino)!=identity):
                raise ValueError('RESTORE_PROBE_REPORT_ROOT_CHANGED')
        check_root()
        descriptor=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,
                           0o600,dir_fd=directory)
        try:
            with os.fdopen(descriptor,'wb') as stream:
                if stream.write(raw)!=len(raw):raise OSError('RESTORE_PROBE_REPORT_SHORT_WRITE')
                stream.flush();os.fsync(stream.fileno())
            guard();check_root()
            os.link(pending,final,src_dir_fd=directory,dst_dir_fd=directory,
                    follow_symlinks=False)
            os.unlink(pending,dir_fd=directory)
            os.fsync(directory)
            check_root()
            receipt=RestoreProbeReceipt(root/final,sha256(raw).hexdigest())
            read_restore_probe_report(receipt,replay,check_active=guard)
            check_root();guard();check_root()
            return receipt
        finally:
            try:os.unlink(pending,dir_fd=directory)
            except FileNotFoundError:pass
    finally:
        os.close(directory)

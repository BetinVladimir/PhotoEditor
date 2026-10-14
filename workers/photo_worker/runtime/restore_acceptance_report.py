"""Durable acceptance report after re-reading all bound source evidence."""
from dataclasses import asdict
from datetime import datetime, timezone
from hashlib import sha256
import os
import re
import stat
from pathlib import Path
from uuid import UUID
from photo_worker.contracts.base import canonical_bytes, parse_json
from .restore_acceptance import RestoreAcceptanceEvidence, PROBE_CHECKS, read_restore_probe_report, _require_restore_guard
from .restore_replay_receipt import VerifiedRestoreReplay, verify_restore_replay_files
from .restore_report import RestoreReportReceipt, _json, _open_report_directory
from .restore_key_installation import _read_private


def persist_restore_acceptance(root, acceptance, recovery_path, replay_path, *,
                               recovery_evidence, check_active, assert_probe_isolation,
                               verify_current_authorities):
    """Never overwrites or unlocks writers. Post-publication failure may leave a report."""
    root=Path(root)
    if (not isinstance(acceptance,RestoreAcceptanceEvidence)
            or acceptance.schema_version!='restore-acceptance/v1' or acceptance.writers_authorized is not False
            or not isinstance(acceptance.replay,VerifiedRestoreReplay)
            or not isinstance(acceptance.replay.run_id,UUID) or acceptance.replay.run_id.int==0
            or not isinstance(acceptance.probes,tuple) or not isinstance(acceptance.probe_receipts,tuple)
            or len(acceptance.probes)!=len(PROBE_CHECKS) or len(acceptance.probe_receipts)!=len(PROBE_CHECKS)
            or not isinstance(acceptance.finished_at,datetime) or acceptance.finished_at.tzinfo is None
            or not all(callable(f) for f in (check_active,assert_probe_isolation,verify_current_authorities))):
        raise ValueError('RESTORE_ACCEPTANCE_REPORT_INSTALLATION')
    def guard():
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
        _require_restore_guard(assert_probe_isolation,'RESTORE_ACCEPTANCE_NOT_ISOLATED')
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
        _require_restore_guard(verify_current_authorities,'RESTORE_ACCEPTANCE_AUTHORITY')
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
    guard()
    if (not root.is_absolute() or root.resolve(strict=True)!=root or not root.is_dir()
            or root.stat().st_uid!=os.getuid() or root.stat().st_mode & 0o022):
        raise ValueError('RESTORE_ACCEPTANCE_REPORT_INSTALLATION')
    def revalidate():
        guard();replay=acceptance.replay
        actual=verify_restore_replay_files(recovery_path,replay.recovery_sha256,replay_path,replay.replay_sha256,
            evidence=recovery_evidence,build_sha256=replay.build_sha256,check_active=guard)
        if actual!=replay:raise ValueError('RESTORE_ACCEPTANCE_REPORT_REPLAY')
        previous=replay.completed_at
        for (kind,checks),expected,receipt in zip(PROBE_CHECKS,acceptance.probes,acceptance.probe_receipts):
            actual=read_restore_probe_report(receipt,replay,check_active=guard)
            if (actual!=expected or actual.kind!=kind or actual.checks!=tuple((name,True) for name in checks)
                    or not previous<=actual.started_at<=actual.finished_at<=acceptance.finished_at):
                raise ValueError('RESTORE_ACCEPTANCE_REPORT_PROBE')
            previous=actual.finished_at
        if acceptance.finished_at>datetime.now(timezone.utc):raise ValueError('RESTORE_ACCEPTANCE_REPORT_TIME')
        guard()
    revalidate()
    # Paths stay in the private operator report, preserving source artifact links.
    raw=canonical_bytes(_json({'schema_version':'restore-acceptance-report/v1',
        'writers_authorized':False,'acceptance':asdict(acceptance)}))
    if len(raw)>262144:raise ValueError('RESTORE_ACCEPTANCE_REPORT_LIMIT')
    stem=str(acceptance.replay.run_id)
    pending=stem+'.acceptance.pending';final=stem+'.acceptance.json'
    directory=_open_report_directory(root,guard)
    try:
        before=os.fstat(directory)
        if (not stat.S_ISDIR(before.st_mode) or before.st_uid!=os.getuid()
                or before.st_mode & 0o022):
            raise ValueError('RESTORE_ACCEPTANCE_REPORT_INSTALLATION')
        identity=(before.st_dev,before.st_ino)
        def check_root():
            current=os.stat(root,follow_symlinks=False)
            if (not stat.S_ISDIR(current.st_mode)
                    or (current.st_dev,current.st_ino)!=identity):
                raise ValueError('RESTORE_ACCEPTANCE_REPORT_ROOT_CHANGED')
        check_root()
        descriptor=os.open(pending,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,
                           0o600,dir_fd=directory)
        try:
            with os.fdopen(descriptor,'wb') as stream:
                if stream.write(raw)!=len(raw):raise OSError('RESTORE_ACCEPTANCE_REPORT_SHORT_WRITE')
                stream.flush();os.fsync(stream.fileno())
            revalidate();check_root()
            os.link(pending,final,src_dir_fd=directory,dst_dir_fd=directory,
                    follow_symlinks=False)
            os.unlink(pending,dir_fd=directory)
            os.fsync(directory)
            check_root();guard();check_root()
            return RestoreReportReceipt(root/final,sha256(raw).hexdigest())
        finally:
            try:os.unlink(pending,dir_fd=directory)
            except FileNotFoundError:pass
    finally:
        os.close(directory)


def verify_restore_acceptance_report(receipt, acceptance, recovery_path, replay_path, *,
                                     recovery_evidence, check_active,
                                     assert_probe_isolation, verify_current_authorities):
    """Re-read the pinned report and all sources before operator approval.

    This validates evidence only; it cannot authorize or resume writers.
    """
    if (not isinstance(receipt,RestoreReportReceipt)
            or not isinstance(receipt.path,Path)
            or not isinstance(acceptance,RestoreAcceptanceEvidence)
            or not isinstance(acceptance.replay,VerifiedRestoreReplay)
            or not isinstance(acceptance.replay.run_id,UUID)
            or not isinstance(receipt.sha256,str)
            or re.fullmatch('[0-9a-f]{64}',receipt.sha256) is None
            or not all(callable(f) for f in (check_active,assert_probe_isolation,verify_current_authorities))):
        raise ValueError('RESTORE_ACCEPTANCE_READER_INSTALLATION')
    def guard():
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
        _require_restore_guard(assert_probe_isolation,'RESTORE_ACCEPTANCE_NOT_ISOLATED')
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
        _require_restore_guard(verify_current_authorities,'RESTORE_ACCEPTANCE_AUTHORITY')
        _require_restore_guard(check_active,'RESTORE_ACCEPTANCE_INACTIVE')
    raw=_read_private(receipt.path,262144,guard)
    if sha256(raw).hexdigest()!=receipt.sha256:
        raise ValueError('RESTORE_ACCEPTANCE_REPORT_PIN')
    value=parse_json(raw,max_bytes=262144)
    expected={'schema_version':'restore-acceptance-report/v1',
              'writers_authorized':False,'acceptance':_json(asdict(acceptance))}
    if (not isinstance(value,dict) or set(value)!=set(expected)
            or value!=expected or canonical_bytes(value)!=raw
            or acceptance.schema_version!='restore-acceptance/v1'
            or acceptance.writers_authorized is not False
            or len(acceptance.probes)!=len(PROBE_CHECKS)
            or len(acceptance.probe_receipts)!=len(PROBE_CHECKS)
            or receipt.path.name!=str(acceptance.replay.run_id)+'.acceptance.json'):
        raise ValueError('RESTORE_ACCEPTANCE_REPORT_BINDING')
    replay=acceptance.replay
    actual=verify_restore_replay_files(recovery_path,replay.recovery_sha256,
        replay_path,replay.replay_sha256,evidence=recovery_evidence,
        build_sha256=replay.build_sha256,check_active=guard)
    if actual!=replay:raise ValueError('RESTORE_ACCEPTANCE_REPORT_REPLAY')
    previous=replay.completed_at
    for (kind,checks),expected_probe,probe_receipt in zip(
            PROBE_CHECKS,acceptance.probes,acceptance.probe_receipts):
        observed=read_restore_probe_report(probe_receipt,replay,check_active=guard)
        if (observed!=expected_probe or observed.kind!=kind
                or observed.checks!=tuple((name,True) for name in checks)
                or not previous<=observed.started_at<=observed.finished_at<=acceptance.finished_at):
            raise ValueError('RESTORE_ACCEPTANCE_REPORT_PROBE')
        previous=observed.finished_at
    if acceptance.finished_at>datetime.now(timezone.utc):
        raise ValueError('RESTORE_ACCEPTANCE_REPORT_TIME')
    if _read_private(receipt.path,262144,guard)!=raw:
        raise ValueError('RESTORE_ACCEPTANCE_REPORT_CHANGED')
    guard()
    return acceptance

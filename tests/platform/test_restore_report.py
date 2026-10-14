from datetime import datetime,timezone
from dataclasses import replace
from hashlib import sha256
from uuid import uuid4
import json
import os
import pytest
from photo_worker.runtime.restore_recovery import RestoreComputeRecoveryEvidence
from photo_worker.runtime.restore_preflight import RestorePreflightEvidence
from photo_worker.runtime.restore_deletion_catalogue import RestoreDatabaseDeletionEvidence
from photo_worker.runtime.restore_deletions import RestoreDeletionEvidence,DeletionHistoryCheckpoint
from photo_worker.runtime.restore_catalogue import RestoreStorageEvidence,RestoreDatabaseEvidence
from photo_worker.runtime.restore_inventory import RestoreInventoryEvidence
from photo_worker.runtime.restore_quarantine import RestoreQuarantineEvidence
from photo_worker.runtime.restore_leases import RestoreLeaseEvidence
from photo_worker.runtime.restore_intents import RestoreIntentEvidence
from photo_worker.runtime.restore_outbox import RestoreOutboxEvidence
from photo_worker.runtime.restore_mail import RestoreMailEvidence
from photo_worker.runtime.restore_cleanup import RestoreCleanupEvidence
from photo_worker.runtime.restore_consent import RestoreConsentEvidence
from photo_worker.runtime.restore_scheduler_grants import RestoreSchedulerGrantEvidence
from photo_worker.runtime.restore_keys import RestoreKeyEvidence
from photo_worker.runtime.restore_report import persist_recovery_report
from photo_worker.runtime.restore_resources import RestoreResourceEvidence
from photo_worker.runtime.restore_quarantine_activate import RestoreQuarantineActivation


def evidence(root,now):
    run=uuid4();head=DeletionHistoryCheckpoint(0,'0'*64)
    deletions=RestoreDatabaseDeletionEvidence('restore-database-deletions/v1','isolated','snapshot',now,
        RestoreDeletionEvidence('restore-deletion-reconciliation/v1',0,head.head_sha256,0))
    inventory=RestoreInventoryEvidence('restore-object-inventory/v1',0,0,'a'*64)
    quarantine=RestoreQuarantineEvidence('restore-quarantine-plan/v1',0,0,0,0,'b'*64)
    storage=RestoreStorageEvidence('restore-storage-verification/v1',
        RestoreDatabaseEvidence('restore-database-objects/v1','isolated','snapshot',now,inventory),quarantine)
    preflight=RestorePreflightEvidence('restore-preflight/v1',run,deletions,storage,root/str(run))
    return RestoreComputeRecoveryEvidence('restore-compute-recovery/v1',preflight,
        RestoreLeaseEvidence('restore-compute-leases/v1','isolated',0,0,0),head,
        RestoreIntentEvidence('restore-expired-intents/v1','isolated',now,0),
        RestoreOutboxEvidence('restore-outbox-preparation/v1','isolated',0,0,0,0,'a'*64),
        RestoreMailEvidence('restore-mail-recovery/v1','isolated',0,0),
        RestoreCleanupEvidence('restore-cleanup-fencing/v1','isolated',()),
        RestoreConsentEvidence('restore-consent-verification/v1',0,'c'*64),
        RestoreSchedulerGrantEvidence('restore-scheduler-grants/v1','isolated',()),
        RestoreKeyEvidence('restore-key-versions/v1','isolated',(('mail','v1'),)),
        RestoreResourceEvidence('restore-resource-recovery/v1','isolated',0,0),
        RestoreQuarantineActivation('restore-quarantine-activation/v1',str(run),'b'*64,0,0))


def test_report_is_private_durable_exclusive_and_never_writer_approval(tmp_path):
    now=datetime.now(timezone.utc);result=evidence(tmp_path,now)
    def write():return persist_recovery_report(tmp_path,result,build_sha256='d'*64,
        backup_at=now,last_wal_lsn='0/ABC',started_at=now,finished_at=now,check_active=lambda:None)
    receipt=write();raw=receipt.path.read_bytes();document=json.loads(raw)
    assert receipt.sha256==sha256(raw).hexdigest()
    assert document['writers_authorized'] is False
    assert document['recovery']['keys']['versions']==[['mail','v1']]
    assert receipt.path.stat().st_mode & 0o077==0
    with pytest.raises(FileExistsError):write()
    assert receipt.path.read_bytes()==raw
    assert not list(tmp_path.glob('*.pending'))


def test_cancel_before_report_publication_leaves_no_final(tmp_path):
    now=datetime.now(timezone.utc);checks=0
    def active():
        nonlocal checks
        checks+=1
        if checks==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):
        persist_recovery_report(tmp_path,evidence(tmp_path,now),build_sha256='d'*64,
            backup_at=now,last_wal_lsn='0/ABC',started_at=now,finished_at=now,check_active=active)
    assert list(tmp_path.iterdir())==[]


def test_inactive_report_does_not_read_report_root(tmp_path,monkeypatch):
    now=datetime.now(timezone.utc)
    original_resolve=type(tmp_path).resolve
    def resolve(path,*args,**kwargs):
        if path==tmp_path:raise AssertionError('report root read before active guard')
        return original_resolve(path,*args,**kwargs)
    monkeypatch.setattr(type(tmp_path),'resolve',resolve)
    with pytest.raises(RuntimeError,match='RESTORE_REPORT_INACTIVE'):
        persist_recovery_report(tmp_path,evidence(tmp_path,now),build_sha256='d'*64,
            backup_at=now,last_wal_lsn='0/ABC',started_at=now,finished_at=now,
            check_active=lambda:False)


@pytest.mark.parametrize('change', ['run', 'digest', 'count', 'bytes'])
def test_report_rejects_quarantine_from_other_recovery(tmp_path,change):
    now=datetime.now(timezone.utc);result=evidence(tmp_path,now)
    activation=result.quarantine
    if change=='run':activation=replace(activation,run_id=str(uuid4()))
    elif change=='digest':activation=replace(activation,plan_sha256='f'*64)
    elif change=='count':activation=replace(activation,versions=1)
    else:activation=replace(activation,bytes=1)
    with pytest.raises(ValueError,match='RESTORE_REPORT_EVIDENCE_IDENTITY'):
        persist_recovery_report(tmp_path,replace(result,quarantine=activation),
            build_sha256='d'*64,backup_at=now,last_wal_lsn='0/ABC',
            started_at=now,finished_at=now,check_active=lambda:None)
    assert list(tmp_path.iterdir())==[]


def test_report_fails_closed_when_root_is_replaced_during_write(tmp_path):
    root=tmp_path/'reports';root.mkdir(mode=0o700)
    moved=tmp_path/'moved';now=datetime.now(timezone.utc);checks=0
    def active():
        nonlocal checks
        checks+=1
        if root.exists() and list(root.glob('*.pending')):
            os.rename(root,moved)
            root.mkdir(mode=0o700)
    with pytest.raises(ValueError,match='RESTORE_REPORT_ROOT_CHANGED'):
        persist_recovery_report(root,evidence(root,now),build_sha256='d'*64,
            backup_at=now,last_wal_lsn='0/ABC',started_at=now,
            finished_at=now,check_active=active)
    assert list(root.iterdir())==[]
    assert list(moved.iterdir())==[]


@pytest.mark.parametrize('after_write', [False, True])
def test_report_refuses_explicit_false_before_publication(tmp_path,after_write):
    now=datetime.now(timezone.utc)
    def active():
        if not after_write or list(tmp_path.glob('*.pending')):
            return False
    with pytest.raises(RuntimeError,match='RESTORE_REPORT_INACTIVE'):
        persist_recovery_report(tmp_path,evidence(tmp_path,now),build_sha256='d'*64,
            backup_at=now,last_wal_lsn='0/ABC',started_at=now,finished_at=now,
            check_active=active)
    assert list(tmp_path.iterdir())==[]

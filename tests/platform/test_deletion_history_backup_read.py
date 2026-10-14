from datetime import datetime,timezone
from uuid import uuid4
import pytest
from photo_worker.runtime.deletion_history_store import DeletionHistoryStore
from photo_worker.runtime.deletion_history_backup import export_deletion_history_backup
from photo_worker.runtime.deletion_history_backup_read import open_deletion_history_backup
from photo_worker.runtime.restore_deletions import DeletionHistoryCheckpoint,verify_restore_deletions


def fixture(tmp_path):
    source=tmp_path/'source';source.mkdir(mode=0o700)
    backups=tmp_path/'backups';backups.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    return export_deletion_history_backup(store,backups,uuid4(),check_active=lambda:None)


def test_pinned_deletion_backup_reads_complete_history_without_writing(tmp_path):
    backup=fixture(tmp_path);before=sorted(p.name for p in backup.directory.iterdir())
    with open_deletion_history_backup(backup.directory,backup.manifest_sha256,backup.checkpoint,check_active=lambda:None) as (head,entries):
        evidence=verify_restore_deletions(entries,head,lambda _:'deleted',check_active=lambda:None)
        assert evidence.sequence==1
    assert sorted(p.name for p in backup.directory.iterdir())==before


@pytest.mark.parametrize('mode',['stale','manifest','database','changed_after_read',
                                 'readable_manifest','readable_database','readable_directory',
                                 'extra_file','extra_after_read'])
def test_pinned_deletion_backup_rejects_stale_or_changed_evidence(tmp_path,mode):
    backup=fixture(tmp_path);head=backup.checkpoint
    if mode=='stale':head=DeletionHistoryCheckpoint(2,'b'*64)
    if mode=='manifest':(backup.directory/'manifest.json').write_bytes(b'changed')
    if mode=='database':(backup.directory/'history.sqlite').write_bytes(b'changed')
    if mode=='readable_manifest':(backup.directory/'manifest.json').chmod(0o640)
    if mode=='readable_database':(backup.directory/'history.sqlite').chmod(0o640)
    if mode=='readable_directory':backup.directory.chmod(0o750)
    if mode=='extra_file':(backup.directory/'unexpected').write_bytes(b'extra')
    with pytest.raises(ValueError):
        with open_deletion_history_backup(backup.directory,backup.manifest_sha256,head,check_active=lambda:None) as (_,entries):
            list(entries)
            if mode=='changed_after_read':(backup.directory/'manifest.json').write_bytes(b'changed')
            if mode=='extra_after_read':(backup.directory/'unexpected').write_bytes(b'extra')

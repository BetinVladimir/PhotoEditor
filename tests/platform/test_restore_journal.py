from dataclasses import replace
from datetime import datetime,timezone
from uuid import uuid4
import json
import pytest
from photo_worker.runtime.restore_inventory import RestoreInventoryEvidence
from photo_worker.runtime.restore_catalogue import RestoreDatabaseEvidence,RestoreStorageEvidence
from photo_worker.runtime.restore_quarantine import RestorePhysicalVersion,plan_restore_quarantine
from photo_worker.runtime.restore_journal import RestoreQuarantineJournal, _rename_exclusive


def evidence(quarantine):
    inventory=RestoreInventoryEvidence('restore-object-inventory/v1',0,0,'a'*64)
    return RestoreStorageEvidence('restore-storage-verification/v1',RestoreDatabaseEvidence(
        'restore-database-objects/v1','isolated','10:20:',datetime.now(timezone.utc),inventory),quarantine)


def test_restore_journal_seals_only_complete_matching_plan(tmp_path):
    run=uuid4()
    with RestoreQuarantineJournal(tmp_path,run,check_active=lambda:None) as journal:
        result=plan_restore_quarantine([], [RestorePhysicalVersion('photos','extra','v',5)],journal.stage,check_active=lambda:None)
        destination=journal.seal(evidence(result))
        assert journal.closed and journal.sealed
    assert destination==tmp_path/str(run)
    marker=json.loads((destination/'complete.json').read_text())
    assert marker['storage']['quarantine']['plan_sha256']==result.plan_sha256
    assert (destination/'extras.jsonl').read_text().count('\n')==1
    assert not (tmp_path/(str(run)+'.incomplete')).exists()
    with pytest.raises(ValueError):RestoreQuarantineJournal(tmp_path,run,check_active=lambda:None)


def test_journal_explicit_false_refuses_before_pending_directory_creation(tmp_path):
    run=uuid4()
    with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
        RestoreQuarantineJournal(tmp_path,run,check_active=lambda:False)
    assert not (tmp_path/(str(run)+'.incomplete')).exists()


def test_journal_explicit_false_poisoned_stage_cannot_seal(tmp_path):
    run=uuid4();active=True
    def guard():return None if active else False
    with RestoreQuarantineJournal(tmp_path,run,check_active=guard) as journal:
        active=False
        with pytest.raises(RuntimeError,match='RESTORE_INACTIVE'):
            journal.stage(RestorePhysicalVersion('photos','extra','v',5))
        active=True
        q=plan_restore_quarantine([],[],lambda _:None,check_active=lambda:None)
        with pytest.raises(ValueError,match='RESTORE_JOURNAL_EVIDENCE_INVALID'):
            journal.seal(evidence(q))
    assert not (tmp_path/str(run)).exists()


@pytest.mark.parametrize('mode',['hash','count','bytes','stage_error','cancel','limit'])
def test_restore_journal_failure_preserves_inactive_pending_plan(tmp_path,mode):
    run=uuid4();cancelled=False
    def active():
        if cancelled:raise RuntimeError('cancelled')
    with RestoreQuarantineJournal(tmp_path,run,check_active=active,maximum_bytes=1 if mode=='limit' else 1<<20) as journal:
        with pytest.raises((ValueError,RuntimeError)):
            q=plan_restore_quarantine([], [RestorePhysicalVersion('photos','extra','v',5)],journal.stage,check_active=lambda:None)
            if mode=='hash':q=replace(q,plan_sha256='b'*64)
            if mode=='count':q=replace(q,extra=2)
            if mode=='bytes':q=replace(q,extra_bytes=6)
            if mode=='stage_error':journal.stage(RestorePhysicalVersion('photos','extra','v',5))
            if mode=='cancel':cancelled=True
            journal.seal(evidence(q))
    assert journal.closed and not journal.sealed
    assert not (tmp_path/str(run)).exists()
    assert (tmp_path/(str(run)+'.incomplete')).is_dir()


@pytest.mark.parametrize('mode',['short_write','write_error','cancel_after_append'])
def test_restore_journal_poisoned_append_cannot_later_seal(tmp_path,mode):
    run=uuid4();checks=0
    def active():
        nonlocal checks
        checks+=1
        if mode=='cancel_after_append' and checks==3:raise RuntimeError('cancelled after append')
    with RestoreQuarantineJournal(tmp_path,run,check_active=active) as journal:
        if mode!='cancel_after_append':
            actual=journal.file
            class BrokenFile:
                def write(self,raw):
                    if mode=='write_error':raise OSError('disk full')
                    actual.write(raw[:3]);return 3
                def close(self):actual.close()
            journal.file=BrokenFile()
        with pytest.raises((OSError,RuntimeError)):
            journal.stage(RestorePhysicalVersion('photos','extra','v',5))
        assert journal.failed
        # Even a matching plan generated after the fault must not rescue the
        # uncertain local stream; recovery requires a new operator run UUID.
        q=plan_restore_quarantine([],[],lambda _:None,check_active=lambda:None)
        with pytest.raises(ValueError):journal.seal(evidence(q))
    assert not (tmp_path/str(run)).exists()


def test_restore_journal_closes_file_when_constructor_directory_sync_fails(tmp_path,monkeypatch):
    import photo_worker.runtime.restore_journal as module
    opened=[];real_fdopen=module.os.fdopen
    def fdopen(*args,**kwargs):
        stream=real_fdopen(*args,**kwargs);opened.append(stream);return stream
    monkeypatch.setattr(module.os,'fdopen',fdopen)
    monkeypatch.setattr(module,'_sync_directory',lambda _:(_ for _ in ()).throw(OSError('fsync failed')))
    with pytest.raises(OSError):RestoreQuarantineJournal(tmp_path,uuid4(),check_active=lambda:None)
    assert opened and all(stream.closed for stream in opened)


def test_exclusive_journal_publish_never_replaces_empty_final_directory(tmp_path):
    pending=tmp_path/'pending'
    final=tmp_path/'final'
    pending.mkdir()
    final.mkdir()
    with pytest.raises(FileExistsError):
        _rename_exclusive(tmp_path,pending.name,final.name)
    assert pending.is_dir() and final.is_dir()
    final.rmdir()
    _rename_exclusive(tmp_path,pending.name,final.name)
    assert final.is_dir() and not pending.exists()


def test_journal_refuses_root_replacement_before_seal(tmp_path):
    root=tmp_path/'journal-root'
    root.mkdir()
    run=uuid4()
    with RestoreQuarantineJournal(root,run,check_active=lambda:None) as journal:
        q=plan_restore_quarantine([],[],journal.stage,check_active=lambda:None)
        root.rename(tmp_path/'moved-root')
        root.mkdir()
        with pytest.raises(ValueError,match='ROOT_CHANGED'):
            journal.seal(evidence(q))
    assert not (root/str(run)).exists()
    assert (tmp_path/'moved-root'/(str(run)+'.incomplete')).is_dir()


def test_exclusive_publish_uses_pinned_root_fd_after_path_moves(tmp_path):
    import os
    root=tmp_path/'original'
    root.mkdir()
    (root/'pending').mkdir()
    descriptor=os.open(root,os.O_RDONLY|os.O_DIRECTORY|os.O_NOFOLLOW)
    try:
        moved=tmp_path/'moved'
        root.rename(moved)
        root.mkdir()
        _rename_exclusive(descriptor,'pending','final')
        assert (moved/'final').is_dir()
        assert not (root/'final').exists()
    finally:os.close(descriptor)


@pytest.mark.parametrize('name',['','.', '..', '../escape', 'nested/path', 'bad\\name', 'bad\x00name'])
def test_exclusive_publish_refuses_nonlocal_names(tmp_path,name):
    with pytest.raises(ValueError,match='OPERATOR_DIRECTORY_NAME'):
        _rename_exclusive(tmp_path,name,'final')
    with pytest.raises(ValueError,match='OPERATOR_DIRECTORY_NAME'):
        _rename_exclusive(tmp_path,'pending',name)


def test_exclusive_publish_refuses_boolean_directory_fd():
    with pytest.raises(ValueError,match='OPERATOR_DIRECTORY_FD'):
        _rename_exclusive(True,'pending','final')


@pytest.mark.parametrize('source_kind',['file','symlink'])
def test_exclusive_publish_requires_real_pending_directory(tmp_path,source_kind):
    source=tmp_path/'pending'
    if source_kind=='file':source.write_bytes(b'not a directory')
    else:
        target=tmp_path/'target'
        target.mkdir()
        source.symlink_to(target,target_is_directory=True)
    with pytest.raises(ValueError,match='OPERATOR_DIRECTORY_SOURCE'):
        _rename_exclusive(tmp_path,'pending','final')
    assert not (tmp_path/'final').exists()

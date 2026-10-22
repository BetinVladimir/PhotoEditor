"""Durable operator approval publication; authored, not execution evidence."""
import os
from pathlib import Path
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.backup_cutover_operator import _exclusive_event


def private_root(tmp_path):
    root=tmp_path/'events';root.mkdir(mode=0o700)
    return root


def test_approval_is_canonical_private_and_idempotent(tmp_path):
    root=private_root(tmp_path);body={'writers_authorized':True,'run_id':'run'}
    path=_exclusive_event(root,'run.approval.json',body)
    before=path.stat()
    assert path.read_bytes()==canonical_bytes(body)
    assert before.st_mode&0o777==0o600 and before.st_nlink==1
    assert _exclusive_event(root,path.name,body)==path
    assert path.stat().st_ino==before.st_ino
    assert sorted(p.name for p in root.iterdir())==[path.name]


def test_conflicting_approval_cannot_replace_original(tmp_path):
    root=private_root(tmp_path)
    path=_exclusive_event(root,'approval.json',{'checkpoint':'original'})
    with pytest.raises(ValueError,match='EVENT_CONFLICT'):
        _exclusive_event(root,path.name,{'checkpoint':'substituted'})
    assert path.read_bytes()==canonical_bytes({'checkpoint':'original'})
    assert list(root.iterdir())==[path]


def test_failed_file_fsync_never_publishes_approval(tmp_path,monkeypatch):
    root=private_root(tmp_path);original=os.fsync;calls=0
    def fail_first(descriptor):
        nonlocal calls
        calls+=1
        if calls==1:raise OSError('injected file durability failure')
        return original(descriptor)
    monkeypatch.setattr(os,'fsync',fail_first)
    with pytest.raises(OSError,match='durability failure'):
        _exclusive_event(root,'approval.json',{'writers_authorized':True})
    assert not list(root.iterdir())


def test_failed_link_never_publishes_or_retains_pending_approval(tmp_path,monkeypatch):
    root=private_root(tmp_path)
    def refuse(*args,**kwargs):raise OSError('injected link failure')
    monkeypatch.setattr(os,'link',refuse)
    with pytest.raises(OSError,match='link failure'):
        _exclusive_event(root,'approval.json',{'writers_authorized':True})
    assert not list(root.iterdir())


@pytest.mark.parametrize('name',['','.', '..','../approval.json','nested/approval.json'])
def test_approval_name_cannot_escape_private_directory(tmp_path,name):
    root=private_root(tmp_path)
    with pytest.raises(ValueError,match='EVENT_NAME'):
        _exclusive_event(root,name,{'writers_authorized':True})
    assert not list(root.iterdir())


def test_approval_rejects_symlink_or_public_root(tmp_path):
    root=private_root(tmp_path);alias=tmp_path/'alias';alias.symlink_to(root,target_is_directory=True)
    with pytest.raises(ValueError,match='EVENT_ROOT'):
        _exclusive_event(alias,'approval.json',{})
    root.chmod(0o755)
    with pytest.raises(ValueError,match='EVENT_ROOT'):
        _exclusive_event(root,'approval.json',{})
    assert not list(root.iterdir())


def test_approval_cannot_follow_existing_final_symlink(tmp_path):
    root=private_root(tmp_path);outside=tmp_path/'outside';outside.write_bytes(b'original');outside.chmod(0o600)
    (root/'approval.json').symlink_to(outside)
    with pytest.raises(ValueError):
        _exclusive_event(root,'approval.json',{'writers_authorized':True})
    assert outside.read_bytes()==b'original'
    assert sorted(p.name for p in root.iterdir())==['approval.json']

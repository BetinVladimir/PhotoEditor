from datetime import datetime,timezone
from hashlib import sha256
from uuid import uuid4
import sqlite3
import os
import pytest
from photo_worker.contracts.base import parse_json
from photo_worker.runtime.deletion_history_store import DeletionHistoryStore
from photo_worker.runtime.deletion_history_backup import export_deletion_history_backup


def test_deletion_history_backup_contains_verified_portable_chain(tmp_path):
    source=tmp_path/'source';source.mkdir(mode=0o700)
    destination=tmp_path/'backup';destination.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None)
    head=store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    result=export_deletion_history_backup(store,destination,uuid4(),check_active=lambda:None)
    raw=(result.directory/'manifest.json').read_bytes();manifest=parse_json(raw,max_bytes=4096)
    assert sha256(raw).hexdigest()==result.manifest_sha256
    assert manifest['sequence']==head.sequence and result.checkpoint==head
    assert sha256((result.directory/'history.sqlite').read_bytes()).hexdigest()==manifest['database_sha256']
    with sqlite3.connect(result.directory/'history.sqlite') as database:
        assert database.execute('SELECT count(*) FROM events').fetchone()[0]==1
    assert len(list(destination.iterdir()))==1


def test_deletion_history_backup_cancelled_export_does_not_publish(tmp_path):
    source=tmp_path/'source';source.mkdir(mode=0o700)
    destination=tmp_path/'backup';destination.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None);calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==2:raise RuntimeError('cancelled')
    with pytest.raises(RuntimeError):export_deletion_history_backup(store,destination,uuid4(),check_active=active)
    assert list(destination.iterdir())==[]


def test_deletion_history_backup_never_replaces_concurrent_empty_final(tmp_path,monkeypatch):
    import photo_worker.runtime.deletion_history_backup as module
    source=tmp_path/'source';source.mkdir(mode=0o700)
    destination=tmp_path/'backup';destination.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    backup_id=uuid4()
    original=module._rename_exclusive
    def raced(root,pending,final):
        os.mkdir(final,dir_fd=root)
        original(root,pending,final)
    monkeypatch.setattr(module,'_rename_exclusive',raced)
    with pytest.raises(FileExistsError):
        export_deletion_history_backup(store,destination,backup_id,check_active=lambda:None)
    assert (destination/str(backup_id)).is_dir()
    assert list((destination/str(backup_id)).iterdir())==[]


def test_deletion_history_backup_rejects_replaced_destination_root(tmp_path):
    source=tmp_path/'source';source.mkdir(mode=0o700)
    destination=tmp_path/'backup';destination.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    moved=False
    def active():
        nonlocal moved
        if moved:return
        pending=list(destination.glob('.deletion-backup-*'))
        if pending and (pending[0]/'manifest.json').exists():
            destination.rename(tmp_path/'moved-backup')
            destination.mkdir(mode=0o700)
            moved=True
    with pytest.raises(ValueError,match='ROOT_CHANGED'):
        export_deletion_history_backup(store,destination,uuid4(),check_active=active)
    assert moved and list(destination.iterdir())==[]
    assert list((tmp_path/'moved-backup').glob('.deletion-backup-*'))


def test_deletion_history_backup_rejects_extra_file_before_publication(tmp_path):
    source=tmp_path/'source';source.mkdir(mode=0o700)
    destination=tmp_path/'backup';destination.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    injected=False
    def active():
        nonlocal injected
        pending=list(destination.glob('.deletion-backup-*'))
        if not injected and pending and (pending[0]/'manifest.json').exists():
            (pending[0]/'unexpected').write_bytes(b'extra')
            injected=True
    with pytest.raises(ValueError,match='CLOSURE'):
        export_deletion_history_backup(store,destination,uuid4(),check_active=active)
    assert injected and list(destination.iterdir())==[]


def test_replaced_root_cleanup_preserves_foreign_same_named_pending(tmp_path):
    source=tmp_path/'source';source.mkdir(mode=0o700)
    destination=tmp_path/'backup';destination.mkdir(mode=0o700)
    store=DeletionHistoryStore(source,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    foreign=None
    def active():
        nonlocal foreign
        if foreign is not None:return
        pending=list(destination.glob('.deletion-backup-*'))
        if pending and (pending[0]/'manifest.json').exists():
            name=pending[0].name
            destination.rename(tmp_path/'original-backup')
            destination.mkdir(mode=0o700)
            foreign=destination/name;foreign.mkdir(mode=0o700)
            (foreign/'owned-by-other-process').write_bytes(b'preserve')
    with pytest.raises(ValueError,match='ROOT_CHANGED'):
        export_deletion_history_backup(store,destination,uuid4(),check_active=active)
    assert foreign is not None
    assert (foreign/'owned-by-other-process').read_bytes()==b'preserve'
    assert list((tmp_path/'original-backup').glob('.deletion-backup-*/manifest.json'))

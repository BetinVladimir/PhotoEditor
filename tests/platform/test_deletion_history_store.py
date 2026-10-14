from datetime import datetime,timezone
from uuid import uuid4
import sqlite3
import pytest
from photo_worker.runtime.deletion_history_store import DeletionHistoryStore
from photo_worker.runtime.restore_deletions import verify_restore_deletions


def test_descriptor_bound_history_survives_root_path_replacement(tmp_path):
    import os
    root = tmp_path / 'private-history'
    root.mkdir(mode=0o700)
    root.chmod(0o700)
    directory = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
    previous = os.open('.', os.O_RDONLY | os.O_DIRECTORY)
    try:
        store = DeletionHistoryStore(None, check_active=lambda:None, root_fd=directory)
        moved = tmp_path / 'moved-history'
        root.rename(moved)
        root.mkdir(mode=0o700)
        store.append(uuid4(), uuid4(), 'requested', datetime.now(timezone.utc))
        assert (moved / 'deletion-history.sqlite').is_file()
        assert not (root / 'deletion-history.sqlite').exists()
    finally:
        os.fchdir(previous)
        os.close(previous)
        os.close(directory)


def test_descriptor_bound_history_rejects_file_and_shared_directory(tmp_path):
    import os
    ordinary = tmp_path / 'ordinary-file'
    ordinary.write_bytes(b'not a directory')
    file_fd = os.open(ordinary, os.O_RDONLY)
    shared = tmp_path / 'shared'
    shared.mkdir(mode=0o700)
    shared.chmod(0o750)
    shared_fd = os.open(shared, os.O_RDONLY | os.O_DIRECTORY)
    try:
        with pytest.raises(ValueError, match='DELETION_HISTORY_INSTALLATION'):
            DeletionHistoryStore(None, check_active=lambda:None, root_fd=file_fd)
        with pytest.raises(ValueError, match='DELETION_HISTORY_INSTALLATION'):
            DeletionHistoryStore(None, check_active=lambda:None, root_fd=shared_fd)
    finally:
        os.close(file_fd)
        os.close(shared_fd)


def test_deletion_store_append_replay_and_full_chain(tmp_path):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    tenant,event=uuid4(),uuid4();now=datetime.now(timezone.utc)
    first=store.append(event,tenant,'requested',now)
    assert store.append(event,tenant,'requested',now)==first
    second=store.append(uuid4(),tenant,'completed',now)
    with store.snapshot() as (head,entries):
        result=verify_restore_deletions(entries,head,lambda _:'deleted',check_active=lambda:None)
        assert head==second and result.sequence==2
    with pytest.raises(ValueError,match='IDEMPOTENCY_CONFLICT'):store.append(event,tenant,'completed',now)


def test_deletion_store_refuses_update_and_delete(tmp_path):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    with sqlite3.connect(store.path) as database:
        for sql in ['DELETE FROM events','UPDATE events SET sha256=sha256']:
            with pytest.raises(sqlite3.IntegrityError):database.execute(sql)


def test_deletion_store_cancellation_rolls_back(tmp_path):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None);calls=0
    def active():
        nonlocal calls
        calls+=1
        if calls==2:raise RuntimeError('cancelled')
    store.active=active
    with pytest.raises(RuntimeError):store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    store.active=lambda:None
    with store.snapshot() as (head,entries):assert head.sequence==0 and list(entries)==[]

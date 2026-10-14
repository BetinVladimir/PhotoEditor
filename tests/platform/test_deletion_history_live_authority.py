from datetime import datetime, timezone
import sqlite3
from uuid import uuid4
import pytest
from photo_worker.runtime.deletion_history_store import DeletionHistoryStore
from photo_worker.runtime.restore_deletions import DeletionHistoryCheckpoint
import photo_worker.runtime.restore_preflight as preflight


def test_live_head_tracks_durable_appends_and_checks_entry_budget(tmp_path):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    assert store.latest_verified_checkpoint()==DeletionHistoryCheckpoint(0,'0'*64)
    first=store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    assert store.latest_verified_checkpoint()==first
    second=store.append(uuid4(),uuid4(),'completed',datetime.now(timezone.utc))
    assert store.latest_verified_checkpoint()==second
    with pytest.raises(ValueError):store.latest_verified_checkpoint(maximum_entries=1)


def test_live_authority_rejects_corrupt_chain_even_when_tail_looks_valid(tmp_path):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    # Simulate operator/storage damage, beyond the normal append-only API.
    with sqlite3.connect(store.path) as database:
        database.execute('DROP TRIGGER events_no_delete')
        database.execute('DELETE FROM events WHERE sequence=1')
    with pytest.raises(ValueError):store.latest_verified_checkpoint()


def test_preflight_installs_real_live_head_reader(tmp_path,monkeypatch):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    first=store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
    def orchestration(*args,**kwargs):
        reader=kwargs['read_latest_deletion_checkpoint']
        assert reader()==first
        second=store.append(uuid4(),uuid4(),'requested',datetime.now(timezone.utc))
        assert reader()==second
        assert 'maximum_history_entries' not in kwargs
        return 'evidence'
    monkeypatch.setattr(preflight,'verify_isolated_restore',orchestration)
    assert preflight.verify_restore_with_live_history(None,None,[],tmp_path,tmp_path,
        uuid4(),tmp_path,'a'*64,history_store=store,expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=lambda:None,
        maximum_history_entries=10)=='evidence'


def test_live_checkpoint_cancellation_refuses_before_snapshot(tmp_path):
    cancelled=False
    def active():
        if cancelled:raise RuntimeError('cancelled')
    store=DeletionHistoryStore(tmp_path,check_active=active)
    cancelled=True
    with pytest.raises(RuntimeError):store.latest_verified_checkpoint()


def test_live_preflight_reader_rejects_explicit_false_after_snapshot(tmp_path,monkeypatch):
    store=DeletionHistoryStore(tmp_path,check_active=lambda:None)
    allowed=True
    def latest_verified_checkpoint(*,maximum_entries):
        nonlocal allowed
        allowed=False
        return DeletionHistoryCheckpoint(0,'0'*64)
    monkeypatch.setattr(store,'latest_verified_checkpoint',latest_verified_checkpoint)
    def orchestration(*args,**kwargs):
        with pytest.raises(RuntimeError,match='RESTORE_PREFLIGHT_INACTIVE'):
            kwargs['read_latest_deletion_checkpoint']()
        return 'refused'
    monkeypatch.setattr(preflight,'verify_isolated_restore',orchestration)
    assert preflight.verify_restore_with_live_history(None,None,[],tmp_path,tmp_path,
        uuid4(),tmp_path,'a'*64,history_store=store,expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=lambda:allowed,
        maximum_history_entries=10)=='refused'

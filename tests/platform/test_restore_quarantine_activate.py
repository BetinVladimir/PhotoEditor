from contextlib import contextmanager
from datetime import datetime,timezone
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_inventory import RestoreInventoryEvidence
from photo_worker.runtime.restore_catalogue import RestoreStorageEvidence,RestoreDatabaseEvidence
from photo_worker.runtime.restore_quarantine import RestorePhysicalVersion,plan_restore_quarantine
from photo_worker.runtime.restore_journal import RestoreQuarantineJournal
from photo_worker.runtime.restore_preflight import RestorePreflightEvidence
from photo_worker.runtime.restore_quarantine_activate import activate_restore_quarantine
import photo_worker.runtime.restore_quarantine_activate as activation_module


@pytest.mark.parametrize('mode',['valid','referenced','truncated','bad_receipt','foreign',
                                 'run_conflict','version_conflict','tamper','swapped_root'])
def test_quarantine_activation_requires_complete_sealed_plan_and_atomic_receipt(tmp_path,mode,monkeypatch):
    run=uuid4();now=datetime.now(timezone.utc)
    with RestoreQuarantineJournal(tmp_path,run,check_active=lambda:None) as journal:
        q=plan_restore_quarantine([], [RestorePhysicalVersion('photos','extra','v1',7)],
            journal.stage,check_active=lambda:None)
        storage=RestoreStorageEvidence('restore-storage-verification/v1',
            RestoreDatabaseEvidence('restore-database-objects/v1','isolated','snapshot',now,
                RestoreInventoryEvidence('restore-object-inventory/v1',0,0,'a'*64)),q)
        path=journal.seal(storage)
    if mode=='truncated':(path/'extras.jsonl').write_bytes(b'')
    if mode=='tamper':
        raw=(path/'extras.jsonl').read_bytes().replace(b'v1',b'v2')
        (path/'extras.jsonl').write_bytes(raw)
    preflight=RestorePreflightEvidence('restore-preflight/v1',run,None,storage,path)
    if mode=='swapped_root':
        original_open=activation_module._open_private_descriptor
        def swapped(target,active):
            moved=tmp_path/'moved'
            path.rename(moved)
            path.symlink_to(moved,target_is_directory=True)
            return original_open(target,active)
        monkeypatch.setattr(activation_module,'_open_private_descriptor',swapped)
    events=[];pending=[];committed=[]
    class Connection:
        info=SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self):
            try:yield
            except BaseException:pending.clear();events.append('rollback');raise
            else:committed.extend(pending);events.append('commit')
        def execute(self,sql,params=None):
            events.append(sql);row=None
            if sql.startswith('SELECT current_database'):row=('foreign' if mode=='foreign' else 'isolated',True)
            elif sql.startswith('INSERT'):pending.append(params)
            elif sql.startswith('SELECT plan_sha256'):row=('0'*64 if mode=='run_conflict' else q.plan_sha256,1,7)
            elif sql.startswith('SELECT EXISTS'):row=(mode=='referenced',)
            elif sql.startswith('SELECT bytes'):row=(8 if mode=='version_conflict' else 7,)
            elif sql.startswith('SELECT count'):row=(0 if mode=='bad_receipt' else 1,7)
            return SimpleNamespace(fetchone=lambda:row)
    def activate():return activate_restore_quarantine(Connection(),preflight,expected_database='isolated',
        assert_isolated_and_paused=lambda:None,check_active=lambda:None)
    if mode=='valid':
        result=activate();assert result.versions==1 and result.bytes==7
        assert len(committed)==2 and 'commit' in events
        lock=next(i for i,sql in enumerate(events) if sql.startswith('SELECT pg_catalog.pg_advisory_xact_lock'))
        ready=next(i for i,sql in enumerate(events) if sql.startswith('SELECT EXISTS'))
        assert events.index('SET TRANSACTION ISOLATION LEVEL READ COMMITTED')<lock<ready
    else:
        with pytest.raises((ValueError,OSError)):activate()
        assert not committed
        if mode=='swapped_root':assert events==[]
    assert not any(sql.startswith('DELETE') for sql in events)

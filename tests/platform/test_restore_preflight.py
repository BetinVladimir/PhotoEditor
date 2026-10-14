from datetime import datetime,timezone
from types import SimpleNamespace
from uuid import uuid4
import pytest
import photo_worker.runtime.restore_preflight as module
from photo_worker.runtime.restore_deletions import DeletionHistoryCheckpoint


@pytest.mark.parametrize('mode',['valid','deletion_failure','storage_failure','checkpoint_changed',
                                 'wrong_database','guard_failure','guard_denied','activity_denied'])
def test_restore_preflight_orders_authorities_and_never_seals_partial_result(tmp_path,monkeypatch,mode):
    events=[];head=DeletionHistoryCheckpoint(1,'a'*64);reads=0
    def latest():
        nonlocal reads
        reads+=1;events.append('checkpoint')
        return DeletionHistoryCheckpoint(2,'b'*64) if mode=='checkpoint_changed' and reads==2 else head
    def guard():
        events.append('guard')
        if mode=='guard_failure':raise RuntimeError('writers active')
        if mode=='guard_denied':return False
    def deletions(*args,**kwargs):
        events.append('deletions')
        if mode=='deletion_failure':raise ValueError('resurrected tenant')
        return SimpleNamespace(database='isolated')
    def storage(*args,**kwargs):
        events.append('storage')
        if mode=='storage_failure':raise ValueError('missing bytes')
        return SimpleNamespace(database=SimpleNamespace(database='production' if mode=='wrong_database' else 'isolated'))
    class Journal:
        def __init__(self,*args,**kwargs):events.append('journal')
        def __enter__(self):return self
        def __exit__(self,*args):events.append('close')
        def stage(self,row):pass
        def seal(self,result):events.append('seal');return tmp_path/'sealed'
    monkeypatch.setattr(module,'verify_restore_deletion_backup',deletions)
    monkeypatch.setattr(module,'verify_minio_restore_storage',storage)
    monkeypatch.setattr(module,'RestoreQuarantineJournal',Journal)
    def run():return module.verify_isolated_restore(None,None,['photos'],tmp_path,tmp_path,uuid4(),tmp_path,'a'*64,expected_database='isolated',assert_isolated_and_paused=guard,read_latest_deletion_checkpoint=latest,check_active=lambda:False if mode=='activity_denied' else None)
    if mode=='valid':
        result=run();assert result.schema_version=='restore-preflight/v1'
        assert events.index('deletions')<events.index('storage')<events.index('seal')
        assert reads==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert 'seal' not in events
        if mode in ('guard_failure','guard_denied','activity_denied','deletion_failure'):
            assert 'storage' not in events
        if mode in ('guard_denied','activity_denied'):
            assert 'checkpoint' not in events and 'journal' not in events

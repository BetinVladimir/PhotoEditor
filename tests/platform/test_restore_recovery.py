from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import pytest
import photo_worker.runtime.restore_recovery as module
from photo_worker.runtime.restore_deletions import DeletionHistoryCheckpoint
from photo_worker.runtime.restore_consent import RestoreConsentCheckpoint


@pytest.mark.parametrize('mode',['valid','preflight_failure','consent_failure','consent_close_failure',
    'fencing_failure','head_changed','foreign_database','intent_failure','outbox_failure',
    'mail_failure','cleanup_failure','scheduler_grant_failure','key_failure','final_fence_failure','resource_failure','quarantine_failure','consent_head_changed','missing_consent_authority'])
def test_recovery_requires_current_consent_before_mutations_and_rechecks_both_heads(tmp_path,monkeypatch,mode):
    events=[];checkpoint=DeletionHistoryCheckpoint(3,'a'*64)
    consent_pin=RestoreConsentCheckpoint(1,'c'*64)
    def preflight(*args,**kwargs):
        events.append('preflight')
        if mode=='preflight_failure':raise ValueError('missing bytes')
        return SimpleNamespace(deletions=SimpleNamespace(history=SimpleNamespace(
            sequence=checkpoint.sequence,head_sha256=checkpoint.head_sha256)))
    @contextmanager
    def current_consents():
        events.append('consent_open')
        try:yield consent_pin,iter([])
        finally:
            events.append('consent_close')
            if mode=='consent_close_failure':raise ValueError('authority changed')
    def consents(*args,**kwargs):
        events.append('consent_verify')
        if mode=='consent_failure':raise ValueError('revoked epoch')
        return SimpleNamespace(grants=consent_pin.grants,sha256=consent_pin.sha256)
    def latest_consent():
        events.append('consent_latest')
        return RestoreConsentCheckpoint(2,'d'*64) if mode=='consent_head_changed' else consent_pin
    def stage(name,failure):
        def execute(*args,**kwargs):
            events.append(name)
            if mode==failure:raise ValueError(failure)
            return SimpleNamespace(database='foreign' if name=='fence' and mode=='foreign_database' else 'isolated')
        return execute
    class History:
        def latest_verified_checkpoint(self,**kwargs):
            events.append('latest')
            return DeletionHistoryCheckpoint(4,'b'*64) if mode=='head_changed' else checkpoint
    monkeypatch.setattr(module,'verify_restore_with_live_history',preflight)
    monkeypatch.setattr(module,'verify_restore_database_consents',consents)
    for attribute,name,failure in (
        ('fence_restored_compute_leases','fence','fencing_failure'),
        ('verify_restored_key_versions','keys','key_failure'),
        ('recover_restored_resources','resources','resource_failure'),
        ('fence_restored_cleanup','cleanup','cleanup_failure'),
        ('fence_restored_scheduler_grants','scheduler_grants','scheduler_grant_failure'),
        ('invalidate_expired_restore_intents','intents','intent_failure'),
        ('recover_restored_mail','mail','mail_failure'),
        ('prepare_restore_outbox','outbox','outbox_failure'),
        ('verify_recovery_fences','final_fences','final_fence_failure'),
        ('activate_restore_quarantine','quarantine','quarantine_failure')):
        monkeypatch.setattr(module,attribute,stage(name,failure))
    def run():return module.recover_verified_restore(None,None,[],tmp_path,tmp_path,
        uuid4(),tmp_path,'a'*64,history_store=History(),expected_database='isolated',
        open_current_consents=None if mode=='missing_consent_authority' else current_consents,
        read_latest_consent_checkpoint=latest_consent,verify_key_version=lambda *args:True,
        assert_isolated_and_paused=lambda:None,check_active=lambda:None)
    if mode=='valid':
        result=run();assert result.deletion_checkpoint==checkpoint
        assert events==['preflight','consent_open','consent_verify','consent_close','keys','fence',
            'resources','scheduler_grants','cleanup','intents','mail','outbox','quarantine','final_fences','latest','consent_latest']
    else:
        with pytest.raises(ValueError):run()
        if mode=='missing_consent_authority':assert events==[]
        if mode=='preflight_failure':assert events==['preflight']
        if mode in ('consent_failure','consent_close_failure'):
            assert events==['preflight','consent_open','consent_verify','consent_close']
        if mode=='head_changed':assert 'consent_latest' not in events
        if mode=='consent_head_changed':assert events[-1]=='consent_latest'
        if mode.endswith('_failure') and mode not in ('preflight_failure','consent_failure','consent_close_failure'):
            assert 'latest' not in events


@pytest.mark.parametrize('denial', ['activity_before_preflight', 'isolation_before_preflight',
                                   'activity_after_preflight', 'isolation_after_preflight'])
def test_recovery_boolean_denial_prevents_later_mutations(tmp_path,monkeypatch,denial):
    events=[]
    allowed=denial!='activity_before_preflight'
    isolated=denial!='isolation_before_preflight'
    checkpoint=DeletionHistoryCheckpoint(3,'a'*64)
    def preflight(*args,**kwargs):
        nonlocal allowed,isolated
        events.append('preflight')
        assert kwargs['check_active']() is None
        assert kwargs['assert_isolated_and_paused']() is None
        if denial=='activity_after_preflight':allowed=False
        if denial=='isolation_after_preflight':isolated=False
        return SimpleNamespace(deletions=SimpleNamespace(history=checkpoint))
    monkeypatch.setattr(module,'verify_restore_with_live_history',preflight)
    def forbidden(*args,**kwargs):
        pytest.fail('recovery reached a mutation after authority denial')
    monkeypatch.setattr(module,'fence_restored_compute_leases',forbidden)
    with pytest.raises(RuntimeError,match='RESTORE_RECOVERY_INACTIVE|RESTORE_WRITERS_NOT_PAUSED'):
        module.recover_verified_restore(None,None,[],tmp_path,tmp_path,uuid4(),tmp_path,
            'a'*64,history_store=object(),expected_database='isolated',
            open_current_consents=forbidden,read_latest_consent_checkpoint=forbidden,
            verify_key_version=forbidden,
            assert_isolated_and_paused=lambda:isolated,
            check_active=lambda:allowed)
    assert events==([] if denial.endswith('before_preflight') else ['preflight'])

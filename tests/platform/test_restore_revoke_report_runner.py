from dataclasses import dataclass
from datetime import datetime,timezone,timedelta
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_revoke_report_runner as m


@dataclass
class Running:
    committed_consent: str='consent'
    barrier_cancelled: bool=True
    running: str='generation'
    cancelled: str='settled'

@dataclass
class Admission:
    positive_batch: object='positive'
    negative_batch: object='negative'
    final_consent: str='consent'


@pytest.mark.parametrize('mode',['valid','bad_pin','running_failure','admission_failure','changed','no_barrier','no_batch','late_result','invalid_admission','false_fixture','false_active'])
def test_combined_report_requires_both_concrete_chains_and_final_consent(monkeypatch,tmp_path,mode):
    replay=m.VerifiedRestoreReplay(uuid4(),'restore','a'*64,'b'*64,'c'*64,1,datetime.now(timezone.utc)-timedelta(seconds=1))
    shared=dict(tenant_id=uuid4(),project_id=uuid4(),actor_id=uuid4())
    target=NS(fixture=NS(**shared,consent_id=uuid4()))
    other=NS(fixture=NS(**shared,consent_id=uuid4()))
    installation=m.RestoreAdmissionRevokeInstallation(target,None,None,None,None,
        m.RestoreRevokeBatchInstallation(None,None,uuid4(),30,b'{}'))
    def validate(*a,**k):
        if mode=='invalid_admission':raise ValueError('RESTORE_REVOKE_RUNNER_BATCH_BINDING')
    monkeypatch.setattr(m,'validate_fixture_revoke_installation',validate)
    if mode=='false_active':
        monkeypatch.setattr(m,'validate_probe_report_root',
            lambda root: (_ for _ in ()).throw(AssertionError('report root read')))
    descriptor={'admission':{'id':'first'},'running':{'id':'second'}}
    monkeypatch.setattr(m,'combined_revoke_descriptor',lambda *a:descriptor)
    events=[]
    def running(*a,**k):
        events.append('running')
        if mode=='running_failure':raise ValueError('running')
        return Running(barrier_cancelled=mode!='no_barrier')
    def admission(*a,**k):
        events.append('admission')
        if mode=='admission_failure':raise ValueError('admission')
        return Admission(positive_batch=None if mode=='no_batch' else 'positive')
    monkeypatch.setattr(m,'run_pinned_running_revoke',running)
    monkeypatch.setattr(m,'run_pinned_revoke_observations',admission)
    monkeypatch.setattr(m,'observe_fixture_learning_consent',lambda *a,**k:'changed' if mode=='changed' else 'consent')
    monkeypatch.setattr(m,'verify_revoke_fixture_independence',lambda *a,**k:None)
    monkeypatch.setattr(m,'observe_fixture_cancelled_render',lambda *a,**k:'changed' if mode=='late_result' else 'settled')
    monkeypatch.setattr(m.RestoreBarrierRenderer,'assert_cancelled',lambda *a:None)
    monkeypatch.setattr(m,'persist_restore_probe_report',lambda *a,**k:events.append('report') or 'receipt')
    pin='0'*64 if mode=='bad_pin' else sha256(canonical_bytes(descriptor)).hexdigest()
    def run():return m.run_revoke_negative_probe(replay,pin,report_root=tmp_path,connection=object(),client=object(),
        buckets=('photos',),admission=installation,running_target=other,child_run_id=uuid4(),maximum_seconds=30,
        renderer_barrier=object.__new__(m.RestoreBarrierRenderer),tls_context=object(),
        assert_installed_fixture=lambda:False if mode=='false_fixture' else None,
        check_active=lambda:False if mode=='false_active' else None)
    if mode=='valid':assert run()=='receipt' and events==['running','admission','report']
    else:
        with pytest.raises(ValueError):run()
        assert 'report' not in events
    if mode in ('bad_pin','invalid_admission','false_fixture','false_active'):assert events==[]

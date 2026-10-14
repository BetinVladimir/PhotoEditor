from dataclasses import replace,asdict
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from datetime import datetime,timezone,timedelta
from uuid import uuid4
import pytest
from photo_worker.runtime import restore_acceptance as module
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from restore_v2_fixture import v2_probe_observations
from restore_legacy_fixture import legacy_probe_observations
from restore_purge_fixture import purge_probe_observations
from restore_revoke_fixture import revoke_probe_observations


@pytest.mark.parametrize('mode',['valid','missing_fixture','failed_check','missing_check','foreign_run',
    'foreign_fixture','foreign_build','naive_time','old_probe','future_probe','runner_error','authority_error','false_authority','false_isolation','false_active','changed_receipt','bad_file_pin','noncanonical_file','changed_probe_file','foreign_replay_report','unknown_report_field','bool_check_as_integer'])
def test_required_probe_cycle_and_no_writer_authorization(monkeypatch,tmp_path,mode):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,datetime.now(timezone.utc)-timedelta(seconds=1))
    events=[];reads=0
    def read(*args,**kwargs):
        nonlocal reads
        reads+=1;kwargs['check_active']()
        return replace(replay,replay_sha256='e'*64) if mode=='changed_receipt' and reads>1 else replay
    monkeypatch.setattr(module,'verify_restore_replay_files',read)
    pins={kind:'d'*64 for kind,_ in module.PROBE_CHECKS}
    v2_pin,v2_observations=v2_probe_observations(replay)
    pins['v2_workflow']=v2_pin
    legacy_pin,legacy_observations=legacy_probe_observations(replay)
    pins['legacy_apply']=legacy_pin
    purge_pin,purge_observations=purge_probe_observations(replay)
    pins['purge_negative']=purge_pin
    revoke_pin,revoke_observations=revoke_probe_observations(replay)
    pins['revoke_negative']=revoke_pin
    if mode=='missing_fixture':pins.pop('revoke_negative')
    def authority():
        if mode=='authority_error':raise RuntimeError('authority unavailable')
        if mode=='false_authority':return False
    def runner(kind,scope,pin,guard):
        events.append(kind);guard()
        if mode=='runner_error':raise RuntimeError('probe failed')
        now=datetime.now(timezone.utc)
        checks=next(checks for name,checks in module.PROBE_CHECKS if name==kind)
        result=module.RestoreProbeEvidence(kind,scope.run_id,scope.database,scope.build_sha256,pin,
            now,now,tuple((name,True) for name in checks),'e'*64)
        if mode=='failed_check':result=replace(result,checks=((checks[0],False),*result.checks[1:]))
        if mode=='missing_check':result=replace(result,checks=result.checks[:-1])
        if mode=='foreign_run':result=replace(result,run_id=uuid4())
        if mode=='foreign_fixture':result=replace(result,fixture_sha256='f'*64)
        if mode=='foreign_build':result=replace(result,build_sha256='f'*64)
        if mode=='naive_time':result=replace(result,started_at=now.replace(tzinfo=None))
        if mode=='old_probe':result=replace(result,started_at=now-timedelta(days=1))
        if mode=='future_probe':result=replace(result,finished_at=now+timedelta(days=1))
        document=asdict(result);document.pop('evidence_sha256')
        document.update(observations=v2_observations if kind=='v2_workflow' else
            legacy_observations if kind=='legacy_apply' else
            purge_observations if kind=='purge_negative' else revoke_observations,
            schema_version='restore-probe-report/v1',recovery_sha256=scope.recovery_sha256,
            replay_sha256=scope.replay_sha256,run_id=str(result.run_id),
            started_at=result.started_at.isoformat(),finished_at=result.finished_at.isoformat())
        if mode=='foreign_replay_report':document['replay_sha256']='f'*64
        if mode=='unknown_report_field':document['extra']=True
        if mode=='bool_check_as_integer':document['checks']=[[name,1] for name,_ in result.checks]
        raw=canonical_bytes(document)
        if mode=='noncanonical_file':raw+=b'\n'
        path=tmp_path/(kind+'.json');path.write_bytes(raw);path.chmod(0o600)
        if mode=='changed_probe_file' and len(events)==4:
            (tmp_path/'legacy_apply.json').write_bytes(b'changed')
        return module.RestoreProbeReceipt(path,'0'*64 if mode=='bad_file_pin' else sha256(raw).hexdigest())
    def run():return module.run_restore_acceptance('recovery','b'*64,'replay','c'*64,evidence=object(),
        build_sha256='a'*64,fixture_pins=pins,run_probe=runner,
        assert_probe_isolation=lambda:False if mode=='false_isolation' else None,
        verify_current_authorities=authority,
        check_active=lambda:False if mode=='false_active' else None)
    if mode=='valid':
        result=run();assert result.writers_authorized is False
        assert events==[kind for kind,_ in module.PROBE_CHECKS] and reads==2
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode not in ('changed_receipt','changed_probe_file'):assert len(events)<=1

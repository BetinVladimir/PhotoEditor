from dataclasses import asdict,replace
from datetime import datetime,timezone
from hashlib import sha256
import json
import os
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_report import persist_recovery_report
from photo_worker.runtime.restore_replay_receipt import verify_restore_replay_files
from photo_worker.runtime.restore_acceptance import (PROBE_CHECKS,RestoreProbeReceipt,
    RestoreAcceptanceEvidence,read_restore_probe_report)
from photo_worker.runtime.restore_acceptance_report import (persist_restore_acceptance,
    verify_restore_acceptance_report)
import photo_worker.runtime.restore_acceptance_report as acceptance_report_module
from test_restore_report import evidence
from restore_v2_fixture import v2_probe_observations
from restore_legacy_fixture import legacy_probe_observations
from restore_purge_fixture import purge_probe_observations
from restore_revoke_fixture import revoke_probe_observations


@pytest.mark.parametrize('mode',['valid','missing_probe','changed_probe','changed_recovery',
    'authority_failure','false_authority','false_isolation','false_active',
    'writer_approval','future_finish','replaced_root','replaced_ancestor'])
def test_acceptance_report_private_bound_and_no_overwrite(tmp_path,mode,monkeypatch):
    now=datetime.now(timezone.utc);recovery=evidence(tmp_path,now)
    report=persist_recovery_report(tmp_path,recovery,build_sha256='d'*64,
        backup_at=now,last_wal_lsn='0/ABC',started_at=now,finished_at=now,check_active=lambda:None)
    replay={'schema_version':'restore-task-replay-receipt/v1','run_id':str(recovery.preflight.run_id),
        'recovery_sha256':report.sha256,'build_sha256':'d'*64,'database':'isolated',
        'initially_pending':0,'confirmed':0,'completed_at':now.isoformat(),'writers_authorized':False}
    replay_path=tmp_path/(str(recovery.preflight.run_id)+'.replay.json')
    raw=canonical_bytes(replay);replay_path.write_bytes(raw);replay_path.chmod(0o600)
    scope=verify_restore_replay_files(report.path,report.sha256,replay_path,sha256(raw).hexdigest(),
        evidence=recovery,build_sha256='d'*64,check_active=lambda:None)
    receipts=[];probes=[]
    v2_pin,v2_observations=v2_probe_observations(scope)
    legacy_pin,legacy_observations=legacy_probe_observations(scope)
    purge_pin,purge_observations=purge_probe_observations(scope)
    revoke_pin,revoke_observations=revoke_probe_observations(scope)
    for kind,checks in PROBE_CHECKS:
        document={'observations':v2_observations if kind=='v2_workflow' else
            legacy_observations if kind=='legacy_apply' else
            purge_observations if kind=='purge_negative' else revoke_observations,
            'schema_version':'restore-probe-report/v1','kind':kind,'run_id':str(scope.run_id),
            'database':scope.database,'build_sha256':scope.build_sha256,
            'fixture_sha256':v2_pin if kind=='v2_workflow' else
                legacy_pin if kind=='legacy_apply' else
                purge_pin if kind=='purge_negative' else revoke_pin,
            'recovery_sha256':scope.recovery_sha256,'replay_sha256':scope.replay_sha256,
            'started_at':now.isoformat(),'finished_at':now.isoformat(),'checks':[[name,True] for name in checks]}
        path=tmp_path/(kind+'.json');raw=canonical_bytes(document);path.write_bytes(raw);path.chmod(0o600)
        receipt=RestoreProbeReceipt(path,sha256(raw).hexdigest());receipts.append(receipt)
        probes.append(read_restore_probe_report(receipt,scope,check_active=lambda:None))
    acceptance=RestoreAcceptanceEvidence('restore-acceptance/v1',scope,tuple(probes),now,probe_receipts=tuple(receipts))
    parent=tmp_path/'parent';parent.mkdir(mode=0o700)
    output=parent/'output';output.mkdir(mode=0o700)
    moved=tmp_path/'moved'
    if mode=='replaced_root':
        original=acceptance_report_module.verify_restore_replay_files
        calls=0
        def replacing_replay(*args,**kwargs):
            nonlocal calls
            calls+=1
            if calls==2:
                os.rename(output,moved)
                output.mkdir(mode=0o700)
            return original(*args,**kwargs)
        monkeypatch.setattr(acceptance_report_module,'verify_restore_replay_files',replacing_replay)
    if mode=='replaced_ancestor':
        original_open=acceptance_report_module._open_report_directory
        def replacing_ancestor(path,guard):
            os.rename(parent,moved)
            parent.symlink_to(moved,target_is_directory=True)
            return original_open(path,guard)
        monkeypatch.setattr(acceptance_report_module,'_open_report_directory',replacing_ancestor)
    if mode=='missing_probe':acceptance=replace(acceptance,probes=acceptance.probes[:-1])
    if mode=='changed_probe':receipts[0].path.write_bytes(b'changed')
    if mode=='changed_recovery':report.path.write_bytes(b'changed')
    if mode=='writer_approval':acceptance=replace(acceptance,writers_authorized=True)
    if mode=='future_finish':acceptance=replace(acceptance,finished_at=now.replace(year=now.year+1))
    if mode=='false_active':
        original_resolve=type(output).resolve
        def resolve(path,*args,**kwargs):
            if path==output:raise AssertionError('report root read before active guard')
            return original_resolve(path,*args,**kwargs)
        monkeypatch.setattr(type(output),'resolve',resolve)
    def authority():
        if mode=='authority_failure':raise RuntimeError('authority unavailable')
        if mode=='false_authority':return False
    def run():return persist_restore_acceptance(output,acceptance,report.path,replay_path,
        recovery_evidence=recovery,
        check_active=lambda:False if mode=='false_active' else None,
        assert_probe_isolation=lambda:False if mode=='false_isolation' else None,
        verify_current_authorities=authority)
    if mode=='valid':
        stored=run();raw=stored.path.read_bytes();document=json.loads(raw)
        assert stored.sha256==sha256(raw).hexdigest()
        assert document['writers_authorized'] is False and document['acceptance']['writers_authorized'] is False
        assert stored.path.stat().st_mode & 0o077==0
        with pytest.raises(FileExistsError):run()
        assert stored.path.read_bytes()==raw
        def verify():return verify_restore_acceptance_report(stored,acceptance,
            report.path,replay_path,recovery_evidence=recovery,check_active=lambda:None,
            assert_probe_isolation=lambda:None,verify_current_authorities=lambda:None)
        assert verify()==acceptance
        with pytest.raises(ValueError,match='RESTORE_ACCEPTANCE_REPORT_BINDING'):
            verify_restore_acceptance_report(stored,replace(acceptance,finished_at=now.replace(year=now.year+1)),
                report.path,replay_path,recovery_evidence=recovery,check_active=lambda:None,
                assert_probe_isolation=lambda:None,verify_current_authorities=lambda:None)
        stored.path.write_bytes(b'changed')
        with pytest.raises(ValueError,match='RESTORE_ACCEPTANCE_REPORT_PIN'):verify()
        stored.path.write_bytes(raw)
        receipts[0].path.write_bytes(b'changed')
        with pytest.raises(ValueError):verify()
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert list(output.iterdir())==[]
        if mode=='replaced_root':assert list(moved.iterdir())==[]

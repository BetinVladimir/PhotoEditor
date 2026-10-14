from dataclasses import dataclass
from datetime import datetime,timezone,timedelta
from hashlib import sha256
from uuid import uuid4
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime import restore_purge_runner as module


@pytest.mark.parametrize('mode',['valid','bad_fixture_pin','start_failure','baseline_failure','request_failure','wait_failure','observe_failure','publish_failure','scope_failure','false_scope','false_active','late_observe','dangling_report'])
def test_purge_runner_orders_actual_stages_and_stops_before_later_work(monkeypatch,tmp_path,mode):
    tenant,job=uuid4(),uuid4();events=[]
    clock=[0.]
    if mode=='late_observe':monkeypatch.setattr(module,'monotonic',lambda:clock[0])
    replay=module.VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,datetime.now(timezone.utc)-timedelta(seconds=1))
    deletion=module.RestoreTenantDeletionTarget('https://restore.test',tenant,7,'Bearer private')
    api=module.RestoreAccessTarget(deletion.origin,'/fixture','/control','Bearer private',403,'FORBIDDEN','d'*64)
    s3=module.RestoreS3AccessTarget('https://storage.test','bucket',tenant,'tenants/'+str(tenant)+'/object','v1','control','v2','d'*64,'private','private')
    refs=tuple(module.RestoreObject('bucket',prefix+'tenants/'+str(tenant)+'/object','v1','d'*64,1) for prefix in ('','staging/','originals/'))
    descriptor=module.purge_fixture_descriptor(replay,['bucket'],refs,deletion,api,'e'*64,s3)
    pin=sha256(canonical_bytes(descriptor)).hexdigest()
    if mode=='false_active':
        monkeypatch.setattr(module,'validate_probe_report_root',
            lambda root: (_ for _ in ()).throw(AssertionError('report root read')))
    if mode=='bad_fixture_pin':pin='0'*64
    if mode=='dangling_report':
        (tmp_path/(str(replay.run_id)+'.purge_negative.pending')).symlink_to(tmp_path/'missing')
    @dataclass(frozen=True)
    class Stage:
        stage: str
    def adapter(name,result):
        def run(*args,**kwargs):
            kwargs['check_active']();events.append(name)
            if mode==name+'_failure':raise RuntimeError('stage failed')
            if mode=='late_observe' and name=='observe':clock[0]=1.
            return result
        return run
    monkeypatch.setattr(module,'verify_fixture_purge_start',adapter('start',None))
    monkeypatch.setattr(module,'verify_purge_fixture_baseline',adapter('baseline',Stage('baseline')))
    monkeypatch.setattr(module,'request_fixture_tenant_deletion',adapter('request',job))
    monkeypatch.setattr(module,'wait_for_fixture_purge',adapter('wait',Stage('completed')))
    monkeypatch.setattr(module,'observe_completed_fixture_purge',adapter('observe',Stage('purged')))
    monkeypatch.setattr(module,'persist_restore_probe_report',adapter('publish','receipt'))
    def scope():
        if mode=='scope_failure':raise RuntimeError('scope invalid')
        if mode=='false_scope':return False
    def run():return module.run_purge_negative_probe(replay,pin,report_root=tmp_path,connection=object(),client=object(),
        buckets=['bucket'],references=refs,deletion_target=deletion,api_target=api,api_sha256='e'*64,s3_target=s3,
        tls_context=object(),maximum_seconds=1,assert_installed_fixture=scope,
        check_active=lambda:False if mode=='false_active' else None)
    if mode=='valid':
        assert run()=='receipt' and events==['start','baseline','start','request','wait','observe','publish']
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        if mode in ('bad_fixture_pin','scope_failure','false_scope','false_active','dangling_report'):assert not events
        elif mode!='publish_failure':assert 'publish' not in events

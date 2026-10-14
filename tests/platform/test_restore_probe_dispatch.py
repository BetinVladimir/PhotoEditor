from datetime import datetime,timezone
from hashlib import sha256
from types import SimpleNamespace as NS
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime import restore_probe_dispatch as m


def test_inactive_dispatch_does_not_read_report_root(monkeypatch,tmp_path):
    def read_root(_):
        raise AssertionError('report root read before active guard')
    monkeypatch.setattr(m,'validate_probe_report_root',read_root)
    dispatch=m.RestoreProbeDispatch.__new__(m.RestoreProbeDispatch)
    object.__setattr__(dispatch,'report_root',tmp_path)
    pins={name:'0'*64 for name,_ in m.PROBE_CHECKS}
    with pytest.raises(ValueError,match='RESTORE_PROBE_DISPATCH_INACTIVE'):
        dispatch.validate_all(None,pins,lambda:False)


@pytest.mark.parametrize('mode',['valid','bad_pin','wrong_order','changed_descriptor',
                                 'changed_pins','fixture_guard','existing_acceptance',
                                 'dangling_probe_symlink','purge_alias','false_fixture',
                                 'false_guard','inactive_before_acceptance'])
def test_installed_probe_dispatch_checks_all_four_before_first_mutation(monkeypatch,tmp_path,mode):
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,1,
        datetime(2020,1,1,tzinfo=timezone.utc))
    events=[];reads=[0]
    def fixture():
        events.append('fixture')
        if mode=='fixture_guard':raise ValueError('fixture unavailable')
        if mode=='false_fixture':return False
    legacy=m.RestoreInstalledLegacyProbe(object(),object(),('photos',),object(),object(),
        b'P'*128,object(),b'K'*32,object(),30,fixture)
    v2=m.RestoreInstalledV2Probe(object(),object(),('photos',),object(),object(),object(),
        b'{}',object(),uuid4(),b'P'*128,object(),30,fixture)
    purge=m.RestoreInstalledPurgeProbe(object(),object(),('photos',),(),object(),object(),
        'd'*64,object(),object(),30,fixture)
    revoke=m.RestoreInstalledRevokeProbe(object(),object(),('photos',),object(),object(),
        uuid4(),30,object(),object(),fixture)
    dispatch=m.RestoreProbeDispatch(tmp_path,legacy,v2,purge,revoke)
    if mode=='existing_acceptance':
        (tmp_path/(str(replay.run_id)+'.acceptance.json')).write_text('existing')
    if mode=='dangling_probe_symlink':
        (tmp_path/(str(replay.run_id)+'.purge_negative.pending')).symlink_to(tmp_path/'missing')
    names=[name for name,_ in m.PROBE_CHECKS]
    shared=str(uuid4());purge_tenant=str(uuid4())
    descriptors={
        'legacy_apply':{'kind':'legacy_apply','legacy':{'preview':{'tenant_id':shared},
            'apply':{'tenant_id':shared}}},
        'v2_workflow':{'kind':'v2_workflow','tenant_id':shared},
        'purge_negative':{'kind':'purge_negative','tenant_id':shared if mode=='purge_alias' else purge_tenant},
        'revoke_negative':{'kind':'revoke_negative','admission':{'fixture':{'tenant_id':shared}},
            'running':{'fixture':{'tenant_id':shared}}}}
    pins={name:sha256(canonical_bytes(descriptors[name])).hexdigest() for name in names}
    active=[True]
    if mode=='inactive_before_acceptance':
        original_lexists=m.os.path.lexists
        def lexists(path):
            if str(path).endswith('.revoke_negative.json'):
                active[0]=False
            return original_lexists(path)
        monkeypatch.setattr(m.os.path,'lexists',lexists)
    if mode=='bad_pin':pins['purge_negative']='0'*64
    monkeypatch.setattr(m,'validate_probe_report_root',lambda root:root)
    def describe(self,scope):
        reads[0]+=1
        if mode=='changed_descriptor' and reads[0]>2:
            return {**descriptors,'v2_workflow':{'changed':True}}
        return descriptors
    monkeypatch.setattr(m.RestoreProbeDispatch,'descriptors',describe)
    for name,func in (('legacy_apply','run_legacy_apply_probe'),
                      ('v2_workflow','run_fixture_v2_workflow_report'),
                      ('purge_negative','run_purge_negative_probe'),
                      ('revoke_negative','run_revoke_negative_probe')):
        monkeypatch.setattr(m,func,lambda *a,_name=name,**k:events.append(_name) or _name)
    runner=dispatch.runner(pins)
    if mode=='changed_pins':
        # Mutation after the detached snapshot is refused.
        pins['legacy_apply']='0'*64
    def run(name):return runner(name,replay,pins[name],
                                lambda:False if mode=='false_guard' or not active[0] else None)
    if mode=='valid':
        assert [run(name) for name in names]==names
        assert [event for event in events if event in names]==names
        assert events[:4]==['fixture']*4
    else:
        with pytest.raises(ValueError):
            if mode in ('wrong_order','changed_descriptor'):
                run(names[0]);run(names[2] if mode=='wrong_order' else names[1])
            else:run(names[0])
        if mode in ('bad_pin','changed_pins','fixture_guard','false_fixture','false_guard',
                    'existing_acceptance','dangling_probe_symlink','purge_alias',
                    'inactive_before_acceptance'):
            assert not any(event in names for event in events)
        else:
            assert [event for event in events if event in names]==[names[0]]

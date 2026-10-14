from datetime import datetime,timezone,timedelta
from hashlib import sha256
from uuid import uuid4
import json
import os
import pytest
from photo_worker.runtime.restore_replay_receipt import VerifiedRestoreReplay
from photo_worker.runtime.restore_probe_report import persist_restore_probe_report
from photo_worker.runtime.restore_acceptance import read_restore_probe_report
from restore_purge_fixture import purge_probe_observations


@pytest.mark.parametrize('mode',['valid','empty_observations','oversized','cancelled','false_active',
    'bad_fixture_pin','tampered_observations'])
def test_probe_report_exclusive_private_bound_observations(tmp_path,mode):
    now=datetime.now(timezone.utc)
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,now-timedelta(seconds=1))
    pin,valid_observations=purge_probe_observations(replay)
    observations={} if mode=='empty_observations' else valid_observations
    if mode=='oversized':observations={'data':'x'*262144}
    if mode=='tampered_observations':
        observations['purge']['storage']['passes']=1
    def active():
        if mode=='cancelled':raise RuntimeError('cancelled')
        if mode=='false_active':return False
    def run():return persist_restore_probe_report(tmp_path,replay,'purge_negative',
        'bad' if mode=='bad_fixture_pin' else pin,now,now,observations,check_active=active)
    if mode=='valid':
        receipt=run();raw=receipt.path.read_bytes();assert receipt.sha256==sha256(raw).hexdigest()
        assert json.loads(raw)['observations']==observations
        assert receipt.path.stat().st_mode & 0o077==0
        with pytest.raises(ValueError,match='RESTORE_PROBE_READER_INACTIVE'):
            read_restore_probe_report(receipt,replay,check_active=lambda:False)
        with pytest.raises(FileExistsError):run()
        assert receipt.path.read_bytes()==raw
    else:
        with pytest.raises((ValueError,RuntimeError)):run()
        assert not list(tmp_path.iterdir())


def test_probe_report_refuses_replaced_output_directory(tmp_path):
    root=tmp_path/'reports';root.mkdir(mode=0o700)
    moved=tmp_path/'moved';now=datetime.now(timezone.utc);calls=0
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,
                                 now-timedelta(seconds=1))
    pin,observations=purge_probe_observations(replay)
    def active():
        nonlocal calls
        calls+=1
        if calls==3:
            os.rename(root,moved)
            root.mkdir(mode=0o700)
    with pytest.raises(ValueError,match='RESTORE_PROBE_REPORT_ROOT_CHANGED'):
        persist_restore_probe_report(root,replay,'purge_negative',pin,
            now,now,observations,check_active=active)
    assert list(root.iterdir())==[]
    assert list(moved.iterdir())==[]


def test_probe_report_false_activity_refuses_before_directory_read(tmp_path,monkeypatch):
    from photo_worker.runtime import restore_probe_report as module
    now=datetime.now(timezone.utc)
    replay=VerifiedRestoreReplay(uuid4(),'isolated','a'*64,'b'*64,'c'*64,0,
                                 now-timedelta(seconds=1))
    pin,observations=purge_probe_observations(replay)
    monkeypatch.setattr(module,'validate_probe_report_root',
                        lambda _: (_ for _ in ()).throw(AssertionError('directory read')))
    with pytest.raises(ValueError,match='RESTORE_PROBE_REPORT_INACTIVE'):
        persist_restore_probe_report(tmp_path,replay,'purge_negative',pin,
                                     now,now,observations,check_active=lambda:False)

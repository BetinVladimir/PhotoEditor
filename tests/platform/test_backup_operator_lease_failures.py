"""Independent physical-target lease failure scenarios; not run evidence."""
from hashlib import sha256
from pathlib import Path
import fcntl
import os
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.backup_operator_lock import restore_operator_lease


def installation(tmp_path):
    fields=('postgres_target','minio_target','config_target','deletions_target')
    settings={'project':'isolated'}
    for name in fields:
        path=tmp_path/name;path.mkdir(mode=0o700);settings[name]=str(path)
    raw=canonical_bytes({'schema_version':'restore-operator-installation/v1',**settings})
    roots={}
    for name in fields:
        path=Path(settings[name]);root=path.parent/('.photo-restore-'+sha256(str(path).encode()).hexdigest())
        root.mkdir(mode=0o700);(root/'installation.json').write_bytes(raw);(root/'installation.json').chmod(0o600)
        roots[name]=root
    settings['operator_state_pin']=sha256(raw).hexdigest()
    return settings,roots


@pytest.mark.parametrize('target',['postgres_target','minio_target','config_target','deletions_target'])
def test_every_physical_target_has_exclusive_lease(tmp_path,target):
    settings,roots=installation(tmp_path)
    descriptor=os.open(roots[target]/'phase.lock',os.O_CREAT|os.O_RDWR,0o600)
    try:
        fcntl.flock(descriptor,fcntl.LOCK_EX|fcntl.LOCK_NB)
        with pytest.raises(BlockingIOError):
            with restore_operator_lease(settings,check_active=lambda:True):
                pytest.fail('shared physical target accepted')
    finally:os.close(descriptor)
    with restore_operator_lease(settings,check_active=lambda:True):pass


def test_partial_acquisition_failure_releases_already_acquired_targets(tmp_path,monkeypatch):
    settings,_=installation(tmp_path);original=fcntl.flock;calls=0
    def injected(descriptor,operation):
        nonlocal calls
        if operation&fcntl.LOCK_EX:
            calls+=1
            if calls==2:raise BlockingIOError('injected second target conflict')
        return original(descriptor,operation)
    with monkeypatch.context() as patch:
        patch.setattr(fcntl,'flock',injected)
        with pytest.raises(BlockingIOError,match='second target conflict'):
            with restore_operator_lease(settings,check_active=lambda:True):
                pytest.fail('partially acquired phase entered')
    with restore_operator_lease(settings,check_active=lambda:True):pass


def test_nested_phase_refuses_replaced_lockfile(tmp_path):
    settings,roots=installation(tmp_path)
    with restore_operator_lease(settings,check_active=lambda:True):
        path=roots['minio_target']/'phase.lock'
        path.unlink();descriptor=os.open(path,os.O_CREAT|os.O_RDWR,0o600);os.close(descriptor)
        with pytest.raises(ValueError,match='LOCK_AUTHORITY'):
            with restore_operator_lease(settings,check_active=lambda:True):
                pytest.fail('replaced lock accepted')


def test_nested_phase_rechecks_installation_identity(tmp_path):
    settings,roots=installation(tmp_path)
    with restore_operator_lease(settings,check_active=lambda:True):
        (roots['config_target']/'installation.json').write_bytes(b'{}')
        with pytest.raises(ValueError,match='STATE_PIN'):
            with restore_operator_lease(settings,check_active=lambda:True):
                pytest.fail('changed identity accepted')

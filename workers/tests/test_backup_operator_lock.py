"""Authored host-lease scenarios; no service or backup command is executed."""
from hashlib import sha256
import os
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.backup_operator_lock import restore_operator_lease


def installed(tmp_path):
    storage=tmp_path/'postgres';storage.mkdir(mode=0o700)
    settings={'project':'isolated','postgres_target':str(storage),
        'minio_target':str(tmp_path/'minio'),'config_target':str(tmp_path/'config'),
        'deletions_target':str(tmp_path/'deletions')}
    raw=canonical_bytes({'schema_version':'restore-operator-installation/v1',**settings})
    for field in ('postgres_target','minio_target','config_target','deletions_target'):
        path=__import__('pathlib').Path(settings[field]);path.mkdir(mode=0o700,exist_ok=True)
        root=tmp_path/('.photo-restore-'+sha256(str(path).encode()).hexdigest());root.mkdir(mode=0o700)
        (root/'installation.json').write_bytes(raw);(root/'installation.json').chmod(0o600)
    root=tmp_path/('.photo-restore-'+sha256(str(storage).encode()).hexdigest())
    settings['operator_state_pin']=sha256(raw).hexdigest()
    return settings,root


def test_same_invocation_can_nest_phase_lease(tmp_path):
    settings,root=installed(tmp_path)
    with restore_operator_lease(settings,check_active=lambda:True):
        with restore_operator_lease(settings,check_active=lambda:True):
            assert (root/'phase.lock').is_file()


def test_external_descriptor_prevents_phase_overlap(tmp_path):
    import fcntl
    settings,root=installed(tmp_path)
    descriptor=os.open(root/'phase.lock',os.O_CREAT|os.O_RDWR,0o600)
    try:
        fcntl.flock(descriptor,fcntl.LOCK_EX|fcntl.LOCK_NB)
        with pytest.raises(BlockingIOError):
            with restore_operator_lease(settings,check_active=lambda:True):pytest.fail('overlap')
    finally:os.close(descriptor)


def test_changed_project_cannot_evade_storage_lease(tmp_path):
    settings,_=installed(tmp_path);settings['project']='another'
    with pytest.raises(ValueError,match='STATE_PIN'):
        with restore_operator_lease(settings,check_active=lambda:True):pytest.fail('changed installation')


def test_symlink_lock_or_unpinned_identity_rejected(tmp_path):
    settings,root=installed(tmp_path)
    (root/'phase.lock').symlink_to(root/'installation.json')
    with pytest.raises(OSError):
        with restore_operator_lease(settings,check_active=lambda:True):pytest.fail('symlink')
    (root/'phase.lock').unlink();settings['operator_state_pin']='0'*64
    with pytest.raises(ValueError,match='STATE_PIN'):
        with restore_operator_lease(settings,check_active=lambda:True):pytest.fail('pin')


def test_cancellation_before_phase_body_releases_lease(tmp_path):
    settings,_=installed(tmp_path)
    with pytest.raises(RuntimeError,match='INACTIVE'):
        with restore_operator_lease(settings,check_active=lambda:False):pytest.fail('cancelled')
    with restore_operator_lease(settings,check_active=lambda:True):pass

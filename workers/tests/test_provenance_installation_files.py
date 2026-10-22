"""Runtime byte locks do not substitute for native loaded-handle attestation."""
from hashlib import sha256
from uuid import uuid4

import pytest

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.provenance import SigningPolicy
from photo_worker.operations.provenance.installation_files import PinnedRuntimeFiles, FileBoundInstallationInspector


def installed(tmp_path):
    path = tmp_path / 'native.bin'
    path.write_bytes(b'qualified-runtime')
    path.chmod(0o600)
    lock = {'schema_version': 'signer-runtime-files/v1', 'files': [
        {'name': 'native', 'sha256': sha256(path.read_bytes()).hexdigest(), 'size_bytes': path.stat().st_size}]}
    digest = sha256(canonical_bytes(lock)).hexdigest()
    return PinnedRuntimeFiles(lock, digest, {'native': path}), path, digest


def test_runtime_open_boolean_denial_prevents_root_open(tmp_path, monkeypatch):
    from photo_worker.operations.provenance import installation_files as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    monkeypatch.setattr(module.os, 'open',
                        lambda *args, **kwargs: pytest.fail('opened after cancellation'))
    with pytest.raises(RuntimeError, match='SIGNER_TASK_INACTIVE'):
        with signing_task_scope(lambda: False):
            module._open_runtime_file(tmp_path / 'native.bin')


def test_runtime_open_boolean_denial_after_leaf_closes_descriptors(tmp_path, monkeypatch):
    from photo_worker.operations.provenance import installation_files as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    import os

    path = tmp_path / 'native.bin'
    path.write_bytes(b'pinned')
    path.chmod(0o600)
    original_open = os.open
    opened = []
    live = True

    def cancel_after_leaf(name, *args, **kwargs):
        nonlocal live
        descriptor = original_open(name, *args, **kwargs)
        opened.append(descriptor)
        if name == path.name:
            live = False
        return descriptor

    monkeypatch.setattr(module.os, 'open', cancel_after_leaf)
    with pytest.raises(RuntimeError, match='SIGNER_TASK_INACTIVE'):
        with signing_task_scope(lambda: live):
            module._open_runtime_file(path)
    assert len(opened) >= 2
    for descriptor in opened:
        with pytest.raises(OSError):
            os.fstat(descriptor)


@pytest.mark.parametrize('mode', ['valid', 'changed', 'symlink', 'hardlink', 'writable', 'directory'])
def test_runtime_file_integrity(tmp_path, mode):
    files, path, _ = installed(tmp_path)
    if mode == 'changed': path.write_bytes(b'replaced-runtime!')
    elif mode == 'symlink':
        other = tmp_path / 'other'
        path.rename(other)
        path.symlink_to(other)
    elif mode == 'hardlink': (tmp_path / 'alias').hardlink_to(path)
    elif mode == 'writable': path.chmod(0o666)
    elif mode == 'directory':
        path.unlink()
        path.mkdir()
    if mode == 'valid': assert files.check() is True
    else:
        with pytest.raises((ValueError, OSError)): files.check()


@pytest.mark.parametrize('mode', ['valid', 'denied', 'truthy', 'replace_during_authority'])
def test_native_authority_still_required_and_files_rechecked(tmp_path, mode):
    files, path, digest = installed(tmp_path)
    policy = SigningPolicy.model_validate_json(canonical_bytes(dict(schema_version=1, id=str(uuid4()), revision='1',
        sdk_lock_sha=digest, key_handle='key', certificate_chain_sha='b'*64,
        timestamp_mode='off', timestamp_service_id=None, allowed_mime=['image/png'],
        trust_bundle_sha='c'*64, revocation_mode='off')))
    def authority(copy, algorithm, cert):
        assert copy is not policy
        if mode == 'replace_during_authority': path.write_bytes(b'replaced-runtime!')
        return True if mode in ('valid', 'replace_during_authority') else 1 if mode == 'truthy' else False
    inspector = FileBoundInstallationInspector(files, authority)
    if mode == 'replace_during_authority':
        with pytest.raises(ValueError): inspector(policy, 'PS256', b'certificate')
    else: assert inspector(policy, 'PS256', b'certificate') is (mode == 'valid')
def test_runtime_file_owner_policy_rejects_foreign_uid():
    from photo_worker.operations.provenance.installation_files import _trusted_runtime_owner
    assert _trusted_runtime_owner(0, 1001)
    assert _trusted_runtime_owner(1001, 1001)
    assert not _trusted_runtime_owner(1002, 1001)
    assert not _trusted_runtime_owner(1002, 0)


@pytest.mark.parametrize('mode', ['symlink_directory', 'writable_directory'])
def test_runtime_file_rejects_unsafe_parent_directory(tmp_path, mode):
    parent = tmp_path / 'installed'
    parent.mkdir()
    binary = parent / 'native.bin'
    binary.write_bytes(b'qualified-runtime')
    binary.chmod(0o600)
    selected = binary
    if mode == 'symlink_directory':
        alias = tmp_path / 'alias'
        alias.symlink_to(parent, target_is_directory=True)
        selected = alias / binary.name
    else:
        parent.chmod(0o777)
    lock = {'schema_version': 'signer-runtime-files/v1', 'files': [
        {'name': 'native', 'sha256': sha256(binary.read_bytes()).hexdigest(),
         'size_bytes': binary.stat().st_size}]}
    files = PinnedRuntimeFiles(lock, sha256(canonical_bytes(lock)).hexdigest(),
                               {'native': selected})
    with pytest.raises((ValueError, OSError)):
        files.check()


def test_runtime_file_rejects_parent_replacement_during_read(tmp_path, monkeypatch):
    from photo_worker.operations.provenance import installation_files as module
    import os

    parent = tmp_path / 'installed'
    parent.mkdir()
    binary = parent / 'native.bin'
    payload = b'qualified-runtime'
    binary.write_bytes(payload)
    binary.chmod(0o600)
    lock = {'schema_version': 'signer-runtime-files/v1', 'files': [
        {'name': 'native', 'sha256': sha256(payload).hexdigest(), 'size_bytes': len(payload)}]}
    files = PinnedRuntimeFiles(lock, sha256(canonical_bytes(lock)).hexdigest(),
                               {'native': binary})
    original_read = os.read
    replaced = False

    def replace_parent(descriptor, size):
        nonlocal replaced
        data = original_read(descriptor, size)
        if not replaced and data:
            replaced = True
            parent.rename(tmp_path / 'retired')
            parent.mkdir()
            replacement = parent / 'native.bin'
            replacement.write_bytes(payload)
            replacement.chmod(0o600)
        return data

    monkeypatch.setattr(module.os, 'read', replace_parent)
    with pytest.raises((ValueError, OSError), match='SIGNER_RUNTIME_CHANGED'):
        files.check()
    assert replaced


def test_runtime_file_cancellation_after_read_closes_descriptor(tmp_path, monkeypatch):
    from photo_worker.operations.provenance import installation_files as module
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    import os

    files, _, _ = installed(tmp_path)
    original_read = os.read
    descriptors = []
    live = True

    def active():
        if not live:
            raise RuntimeError('cancelled')

    def cancel_after_read(descriptor, size):
        nonlocal live
        data = original_read(descriptor, size)
        descriptors.append(descriptor)
        live = False
        return data

    monkeypatch.setattr(module.os, 'read', cancel_after_read)
    with pytest.raises(RuntimeError, match='cancelled'):
        with signing_task_scope(active):
            files.check()
    assert len(descriptors) == 1
    with pytest.raises(OSError):
        os.fstat(descriptors[0])


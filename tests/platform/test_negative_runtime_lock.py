import platform
import sys
from importlib import metadata
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.optical import negative_runtime_lock as module
from photo_worker.runtime.executor import OperationFailure


def lock():
    return module.NegativeRuntimeLock(schema_version=1,epsilon=1e-6,
        python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},
        components=module.installed_component_hashes())


def test_actual_negative_runtime_and_epsilon_are_pinned():
    original=lock();sha=canonical_hash(original)
    assert module.verify_installed_runtime(original,sha)==sha
    changed=original.model_copy(deep=True);changed.epsilon=1e-7
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        module.verify_installed_runtime(changed,sha)


def test_wrong_component_bytes_refused_even_with_matching_manifest_hash():
    current=lock();current.components['negative_algorithm']='0'*64
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        module.verify_installed_runtime(current,canonical_hash(current))


@pytest.mark.parametrize('mode',['ok','symlink','hardlink','noncanonical','hash','oversize','directory'])
def test_negative_runtime_file_loader(tmp_path,mode):
    import os
    from hashlib import sha256
    from photo_worker.contracts.base import canonical_bytes
    raw=canonical_bytes(lock());path=tmp_path/'runtime.json'
    if mode=='noncanonical':raw+=b' '
    if mode=='oversize':raw=b' '*65537
    path.write_bytes(raw)
    digest=sha256(raw).hexdigest()
    if mode=='symlink':
        target=tmp_path/'link.json';target.symlink_to(path);path=target
    if mode=='hardlink':os.link(path,tmp_path/'other.json')
    if mode=='directory':path=tmp_path
    if mode=='hash':digest='0'*64
    if mode=='ok':assert module.load_negative_runtime_lock(path,digest).epsilon==1e-6
    else:
        with pytest.raises((OSError,ValueError,OperationFailure)):module.load_negative_runtime_lock(path,digest)

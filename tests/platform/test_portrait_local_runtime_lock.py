from importlib import metadata
import platform
import sys

import pytest

from photo_worker.contracts.base import canonical_hash,canonical_bytes
from photo_worker.operations.portrait import local_runtime_lock as runtime
from photo_worker.runtime.executor import OperationFailure


@pytest.fixture(scope='module')
def pinned():
    return runtime.LocalPortraitRuntimeLock(schema_version=1,
        python_version='.'.join(map(str,sys.version_info[:3])),implementation=platform.python_implementation(),machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','opencv-python-headless')},components=runtime.installed_component_hashes())


def test_local_runtime_actual_inventory_and_roundtrip(pinned):
    assert runtime.verify_installed_runtime(pinned,canonical_hash(pinned))==canonical_hash(pinned)
    assert {'opencv_binary','local_algorithm','local_tiles','manual_raster','local_manual_admission','local_result','runtime_database','local_runtime_lock'}<=pinned.components.keys()


@pytest.mark.parametrize('component',['opencv_binary','local_algorithm','manual_raster','runtime_database'])
def test_local_runtime_detects_changed_implementation(monkeypatch,pinned,component):
    altered=dict(pinned.components);altered[component]='0'*64
    monkeypatch.setattr(runtime,'installed_component_hashes',lambda:altered)
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        runtime.verify_installed_runtime(pinned,canonical_hash(pinned))


def test_local_runtime_rejects_wrong_pin_and_package(pinned):
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        runtime.verify_installed_runtime(pinned,'0'*64)
    changed=pinned.model_copy(update={'packages':dict(pinned.packages,numpy='0.0.0')})
    with pytest.raises(OperationFailure,match='RUNTIME_UNQUALIFIED'):
        runtime.verify_installed_runtime(changed,canonical_hash(changed))


@pytest.mark.parametrize('kind',['regular','symlink','hardlink','noncanonical','wrong_pin','oversized','fifo'])
def test_local_runtime_lock_file_identity(tmp_path,pinned,kind):
    import os
    path=tmp_path/'portrait-runtime.lock.json'
    path.write_bytes(canonical_bytes(pinned))
    digest=canonical_hash(pinned)
    if kind=='symlink':
        link=tmp_path/'linked.json';link.symlink_to(path);path=link
    elif kind=='hardlink':os.link(path,tmp_path/'second.json')
    elif kind=='noncanonical':path.write_bytes(b' '+path.read_bytes())
    elif kind=='wrong_pin':digest='0'*64
    elif kind=='oversized':path.write_bytes(b' '*65537)
    elif kind=='fifo':path.unlink();os.mkfifo(path)
    if kind=='regular':
        assert runtime.load_local_portrait_runtime_lock(path,digest)==pinned
    else:
        with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
            runtime.load_local_portrait_runtime_lock(path,digest)

from importlib import metadata
import platform
import sys
import pytest
from functools import lru_cache
from photo_worker.contracts.base import canonical_hash,canonical_bytes
from photo_worker.operations.portrait import donor_runtime_lock as runtime
from photo_worker.runtime.executor import OperationFailure


@lru_cache(maxsize=1)
def current_runtime():
    return runtime.DonorPortraitRuntimeLock(schema_version=1,
        python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',
        machine=platform.machine(),packages={name:metadata.version(name)
            for name in ('numpy','pydantic','rfc8785','opencv-python-headless')},
        components=runtime.installed_component_hashes())


@pytest.fixture(scope='module')
def pinned():return current_runtime()


def test_actual_donor_runtime_inventory(pinned):
    assert runtime.verify_installed_runtime(pinned,canonical_hash(pinned))==canonical_hash(pinned)
    assert {'portrait_donor','portrait_donor_manual','donor_files','donor_result',
        'portrait_open_eyes_manual_stage','array_evidence','opencv_binary'}<=pinned.components.keys()


@pytest.mark.parametrize('component',['portrait_donor','portrait_open_eyes_manual_stage','opencv_binary'])
def test_changed_runtime_bytes_rejected(monkeypatch,pinned,component):
    hashes=dict(pinned.components);hashes[component]='0'*64
    monkeypatch.setattr(runtime,'installed_component_hashes',lambda:hashes)
    with pytest.raises(OperationFailure,match='RUNTIME_BYTES_MISMATCH'):
        runtime.verify_installed_runtime(pinned,canonical_hash(pinned))


def test_actual_bounded_lock_file_roundtrip(tmp_path,pinned):
    path=tmp_path/'donor.json';path.write_bytes(canonical_bytes(pinned))
    assert runtime.load_donor_portrait_runtime_lock(path,canonical_hash(pinned))==pinned


@pytest.mark.parametrize('kind',['symlink','noncanonical','oversized','wrong_sha'])
def test_invalid_lock_file_rejected(tmp_path,pinned,kind):
    path=tmp_path/'donor.json';path.write_bytes(canonical_bytes(pinned))
    expected=canonical_hash(pinned)
    if kind=='symlink':
        link=tmp_path/'link.json';link.symlink_to(path);path=link
    if kind=='noncanonical':path.write_bytes(canonical_bytes(pinned)+b' ')
    if kind=='oversized':path.write_bytes(b' '*65537)
    if kind=='wrong_sha':expected='0'*64
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        runtime.load_donor_portrait_runtime_lock(path,expected)


def test_runtime_inventory_pins_complete_donor_execution_authority():
    assert {'portrait_open_eyes_execute','portrait_donor_input_documents',
        'portrait_donor_reader','portrait_donor_object_reader','portrait_donor_attempt',
        'portrait_donor_source','portrait_donor_manual_authority',
        'portrait_donor_qualification','portrait_donor_qualification_loader',
        'donor_handoff','adoption_store','render_replay_reader'} <= runtime.component_paths().keys()

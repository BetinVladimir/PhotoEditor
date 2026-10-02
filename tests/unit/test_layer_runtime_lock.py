from importlib import metadata
import platform
import sys
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.operations.layers import runtime_lock as runtime
from photo_worker.runtime.executor import OperationFailure


def lock():
    return runtime.LayerRuntimeLock(schema_version=1,
        python_version='.'.join(map(str,sys.version_info[:3])), implementation='CPython',
        machine=platform.machine(),packages={n:metadata.version(n) for n in ('numpy','pydantic','rfc8785')},
        components=runtime.installed_component_hashes())


def test_actual_runtime_bytes_match_operator_pin():
    pinned=lock()
    assert runtime.verify_installed_runtime(pinned,canonical_hash(pinned))==canonical_hash(pinned)


@pytest.mark.parametrize('change,code', [('pin','RUNTIME_LOCK_MISMATCH'),
    ('package','RUNTIME_UNQUALIFIED'),('machine','RUNTIME_UNQUALIFIED'),('component','RUNTIME_BYTES_MISMATCH')])
def test_changed_runtime_is_rejected(change,code):
    pinned=lock();expected=canonical_hash(pinned)
    if change=='pin':expected='0'*64
    else:
        data=pinned.model_dump()
        if change=='package':data['packages']['numpy']='0.0.0'
        elif change=='machine':data['machine']='other-machine'
        else:data['components']['layer_prepare']='0'*64
        pinned=runtime.LayerRuntimeLock.model_validate(data);expected=canonical_hash(pinned)
    with pytest.raises(OperationFailure) as caught:runtime.verify_installed_runtime(pinned,expected)
    assert caught.value.error.code==code


def test_missing_component_cannot_be_qualified():
    data=lock().model_dump();del data['components']['layer_masks']
    with pytest.raises(ValueError):runtime.LayerRuntimeLock.model_validate(data)


def test_loader_reads_pinned_regular_file(tmp_path):
    from photo_worker.contracts.base import canonical_bytes
    pinned=lock();path=tmp_path/'runtime.json';path.write_bytes(canonical_bytes(pinned))
    assert runtime.load_layer_runtime_lock(path,canonical_hash(pinned))==pinned


@pytest.mark.parametrize('kind',['symlink','hardlink','directory','oversized','duplicate'])
def test_loader_rejects_unsafe_or_ambiguous_file(tmp_path,kind):
    import os
    from photo_worker.contracts.base import canonical_bytes
    pinned=lock();path=tmp_path/'runtime.json';path.write_bytes(canonical_bytes(pinned))
    target=path
    if kind=='symlink':
        target=tmp_path/'link';target.symlink_to(path)
    elif kind=='hardlink':os.link(path,tmp_path/'second')
    elif kind=='directory':target=tmp_path
    elif kind=='oversized':path.write_bytes(b' '*65537)
    else:path.write_bytes(b'{"schema_version":1,"schema_version":1}')
    with pytest.raises((OSError,OperationFailure)):
        runtime.load_layer_runtime_lock(target,canonical_hash(pinned))

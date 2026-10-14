from importlib import metadata
import platform
import sys
import pytest
from photo_worker.contracts.base import canonical_hash
from photo_worker.v2.render_runtime import RenderRuntimeLock,installed_component_hashes,verify_installed_runtime
from photo_worker.runtime.executor import OperationFailure


def lock():
    return RenderRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),
        implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name)
        for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())


def test_actual_installed_composition_bytes_match_operator_pinned_digest():
    value=lock()
    assert verify_installed_runtime(value,canonical_hash(value))==canonical_hash(value)
    assert 'render_adoption' in value.components and 'render_execute' in value.components
    assert 'contract_override' in value.components
    assert 'render_recomposition_adoption' in value.components
    assert 'render_adoption_inputs' in value.components
    assert 'render_prepare_adoption' in value.components
    assert 'adoption_service' in value.components
    assert 'render_replay_reader' in value.components
    assert 'render_replay_artifacts' in value.components
    assert 'render_profiled_replay' in value.components
    assert 'render_qualification_pin' in value.components
    assert 'render_cache_identity' in value.components
    assert 'runtime_service' in value.components
    assert 'adopter_service' in value.components


@pytest.mark.parametrize('field',['digest','component','recomposition','runtime_service','adopter_service','package','machine'])
def test_changed_build_or_host_cannot_match_pinned_runtime(field):
    value=lock();expected=canonical_hash(value)
    if field=='digest':expected='0'*64
    elif field=='component':value.components['render_execute']='0'*64;expected=canonical_hash(value)
    elif field=='recomposition':value.components['render_recomposition_adoption']='0'*64;expected=canonical_hash(value)
    elif field in ('runtime_service','adopter_service'):value.components[field]='0'*64;expected=canonical_hash(value)
    elif field=='package':value.packages['numpy']='not-installed';expected=canonical_hash(value)
    else:value.machine='other-machine';expected=canonical_hash(value)
    with pytest.raises(OperationFailure):verify_installed_runtime(value,expected)


def test_incomplete_component_inventory_rejected():
    value=lock();del value.components['render_style']
    with pytest.raises(ValueError):RenderRuntimeLock.model_validate(value.model_dump())


def test_pinned_runtime_lock_loaded_from_actual_canonical_file(tmp_path):
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.render_runtime import load_render_runtime_lock
    value=lock();path=tmp_path/'render-lock.json';path.write_bytes(canonical_bytes(value))
    assert load_render_runtime_lock(path,canonical_hash(value))==value


@pytest.mark.parametrize('kind',['symlink','hardlink','oversize','noncanonical','missing','wrong_pin','duplicate'])
def test_invalid_runtime_file_rejected(kind,tmp_path):
    import os
    from photo_worker.contracts.base import canonical_bytes
    from photo_worker.v2.render_runtime import load_render_runtime_lock
    value=lock();path=tmp_path/'render-lock.json';path.write_bytes(canonical_bytes(value));expected=canonical_hash(value)
    if kind=='symlink':
        link=tmp_path/'link';link.symlink_to(path);path=link
    elif kind=='hardlink':os.link(path,tmp_path/'hardlink')
    elif kind=='oversize':path.write_bytes(b' '*65537)
    elif kind=='noncanonical':path.write_bytes(b' '+path.read_bytes())
    elif kind=='missing':path.unlink()
    elif kind=='wrong_pin':expected='0'*64
    else:path.write_bytes(b'{"schema_version":1,"schema_version":1}')
    with pytest.raises(OperationFailure,match='RUNTIME_LOCK_MISMATCH'):
        load_render_runtime_lock(path,expected)

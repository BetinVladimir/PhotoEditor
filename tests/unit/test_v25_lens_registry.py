from hashlib import sha256
import platform,sys
from importlib import metadata
import pytest
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.optical.registry import LensRegistry,load_lens_registry
from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,installed_component_hashes
from photo_worker.runtime.executor import OperationFailure


def test_registry_raw_sha_runtime_and_symlink_boundary(tmp_path):
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    registry=LensRegistry(schema_version=1,runtime=runtime,profiles=[])
    raw=canonical_bytes(registry);path=tmp_path/'lens.json';path.write_bytes(raw)
    loaded=load_lens_registry(path,sha256(raw).hexdigest())
    assert loaded==registry and loaded.admission_pins()==()
    with pytest.raises(OperationFailure):load_lens_registry(path,'a'*64)
    path.write_bytes(raw+b' ')
    with pytest.raises(OperationFailure):load_lens_registry(path,sha256(raw).hexdigest())
    link=tmp_path/'link.json';link.symlink_to(path)
    with pytest.raises(OSError):load_lens_registry(link,sha256(raw).hexdigest())

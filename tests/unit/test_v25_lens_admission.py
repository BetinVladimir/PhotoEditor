from contextlib import contextmanager
from types import SimpleNamespace as NS
from uuid import uuid4
import pytest
from photo_worker.contracts.base import ArtifactRef,canonical_hash
from photo_worker.operations.optical.admission import PinnedLensAdmission,QualifiedLensPin,LensMetadataEvidence
from photo_worker.operations.optical.contracts import OpticalLensParams
from photo_worker.runtime.executor import OperationFailure
from test_v25_lens_map import profile


def test_pinned_admission_requires_exact_profile_and_bound_metadata():
    selected=profile();source=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    params=OpticalLensParams(schema_version=1,base={'asset_version_id':uuid4(),'source':source,'canvas_hash':'b'*64},strength=1.,profile=source,focal_mm=50,aperture=2)
    values=selected.model_dump(mode='json');values.pop('qualification')
    import platform,sys
    from importlib import metadata
    from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,installed_component_hashes
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    runtime_sha=canonical_hash(runtime)
    pin=QualifiedLensPin(selected.qualification,canonical_hash(values),runtime_sha,'d'*64)
    metadata=LensMetadataEvidence(params.base.asset_version_id,source,'Camera','Lens',50,2)
    class Artifacts:
        opened=0
        @contextmanager
        def input(self,ref,suffix):
            assert ref==selected.qualification;self.opened+=1;yield NS(path='verified-test-artifact')
    artifacts=Artifacts();ctx=NS(check_active=lambda:None)
    provider=PinnedLensAdmission(artifacts,[pin],lambda binding:metadata,runtime_sha,runtime_lock=runtime)
    assert provider.admit(ctx,params,selected)==('Camera','Lens',50,2)
    assert artifacts.opened==1
    with pytest.raises(OperationFailure):provider.admit(ctx,params,selected.model_copy(update={'k':[.1,0,0]}))
    wrong=LensMetadataEvidence(uuid4(),source,'Camera','Lens',50,2)
    provider=PinnedLensAdmission(artifacts,[pin],lambda binding:wrong,runtime_sha,runtime_lock=runtime)
    with pytest.raises(OperationFailure):provider.admit(ctx,params,selected)
    provider=PinnedLensAdmission(artifacts,[pin,pin],lambda binding:metadata,runtime_sha,runtime_lock=runtime)
    with pytest.raises(OperationFailure):provider.admit(ctx,params,selected)


def test_runtime_lock_refuses_changed_hash_and_installed_version():
    import platform,sys
    from importlib import metadata
    from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,verify_installed_runtime,installed_component_hashes
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    assert verify_installed_runtime(runtime,canonical_hash(runtime))==canonical_hash(runtime)
    with pytest.raises(OperationFailure):verify_installed_runtime(runtime,'a'*64)
    changed=runtime.model_copy(update={'packages':{**runtime.packages,'numpy':'0.0.0'}})
    with pytest.raises(OperationFailure):verify_installed_runtime(changed,canonical_hash(changed))


@pytest.mark.parametrize("component", ["lens_algorithm", "lens_metadata_source", "lens_exif", "lens_admission"])
def test_runtime_bytes_mismatch_is_not_hidden_by_matching_versions(component):
    import platform,sys
    from importlib import metadata
    from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,verify_installed_runtime,installed_component_hashes
    components=installed_component_hashes();components[component]='a'*64
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=components)
    with pytest.raises(OperationFailure):verify_installed_runtime(runtime,canonical_hash(runtime))

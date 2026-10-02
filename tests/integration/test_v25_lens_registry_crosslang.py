"""Worker/backend registry parity; synthetic pins are not photo qualification."""
from pathlib import Path
from hashlib import sha256
from uuid import uuid4
from importlib import metadata
import os
import platform
import subprocess
import sys
import tempfile
from photo_worker.contracts.base import ArtifactRef,canonical_bytes,canonical_hash
from photo_worker.operations.optical.contracts import LensProfile
from photo_worker.operations.optical.registry import LensRegistry,LensRegistryPin,load_lens_registry
from photo_worker.operations.optical.runtime_lock import OpticalRuntimeLock,installed_component_hashes

ROOT=Path(__file__).resolve().parents[2]


def test_python_worker_registry_and_go_backend_hash_parity():
    runtime=OpticalRuntimeLock(schema_version=1,python_version='.'.join(map(str,sys.version_info[:3])),implementation='CPython',
        machine=platform.machine(),packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785')},components=installed_component_hashes())
    qualification=ArtifactRef(artifact_id=uuid4(),sha256='a'*64)
    profile=LensProfile(schema_version=1,camera_model='Камера α',lens_model='Обектив β',focal_mm=50,aperture=2,
        cx=.5,cy=.5,k=[.01,-.0001,0],p=[0,0],vignette=[.01,0,0],ca_red_scale=1.0001,ca_blue_scale=.9999,qualification=qualification)
    coefficients=profile.model_dump(mode='json');coefficients.pop('qualification')
    registry=LensRegistry(schema_version=1,runtime=runtime,profiles=[LensRegistryPin(qualification=qualification,
        coefficients_sha=canonical_hash(coefficients),runtime_lock_sha=canonical_hash(runtime),dataset_sha='b'*64)])
    root=ROOT/'.runtime';root.mkdir(exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='lens-parity-',dir=root) as directory:
        directory=Path(directory);registry_path=directory/'registry.json';profile_path=directory/'profile.json'
        raw=canonical_bytes(registry);registry_path.write_bytes(raw);profile_raw=canonical_bytes(profile);profile_path.write_bytes(profile_raw)
        digest=sha256(raw).hexdigest()
        assert load_lens_registry(registry_path,digest)==registry
        env=dict(os.environ,PHOTO_TEST_LENS_REGISTRY=str(registry_path),PHOTO_TEST_LENS_REGISTRY_SHA=digest,
            PHOTO_TEST_LENS_RUNTIME_SHA=canonical_hash(runtime),PHOTO_TEST_LENS_PROFILE=str(profile_path),
            PHOTO_TEST_LENS_PROFILE_ID=str(uuid4()),PHOTO_TEST_LENS_PROFILE_SHA=sha256(profile_raw).hexdigest(),GOCACHE=str(root/'go-cache'))
        result=subprocess.run(['go','test','./internal/v2input','-run','^TestIntegrationPythonLensRegistry$',
            '-count=1','-timeout=60s','-v'],cwd=ROOT/'backend',env=env,capture_output=True,text=True,timeout=70)
        assert result.returncode==0,result.stdout+result.stderr

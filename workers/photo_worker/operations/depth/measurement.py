"""Installed depth runtime and device measurements; never self-qualification."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
from photo_worker.contracts.base import canonical_hash
from photo_worker.models.install import ModelError,verify_model
from photo_worker.runtime.executor import OperationFailure


def component_paths():
    # Reuse native library inventory, replacing the operation-specific modules.
    from photo_worker.operations.mask.person_measurement import component_paths as shared
    paths={key:path for key,path in shared().items() if not key.startswith('mask_') and key!='contracts_person'}
    root=Path(__file__).resolve().parents[2]
    for name in ('predict','authority','measurement','prepare'):
        paths['depth_'+name]=root/'operations/depth'/(name+'.py')
    paths['contracts_depth']=root/'contracts/depth.py'
    paths['depth_startup_config']=root/'runtime/depth_config.py'
    from photo_worker.models.processing_inventory import processing_component_paths
    paths.update(processing_component_paths())
    return paths


def measure_runtime():
    import torch
    components={}
    for name,path in component_paths().items():
        digest=sha256()
        with path.open('rb') as stream:
            for block in iter(lambda:stream.read(1048576),b''):digest.update(block)
        components[name]=digest.hexdigest()
    return canonical_hash(dict(schema_version=1,operation='depth.predict',python_version=platform.python_version(),
        implementation=platform.python_implementation(),machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','torch','torchvision','transformers','Pillow')},
        torch_build=sha256(torch.__config__.show().encode()).hexdigest(),components=components))


def installed_depth_measurements(pool,backend):
    from photo_worker.operations.mask.person_measurement import measure_device
    entries=[entry for entry in pool.lock.models if entry.id=='depth_anything_v2_small']
    if len(entries)!=1:raise OperationFailure('DEPTH_MODEL_UNAVAILABLE')
    try:
        verify_model(pool.root/pool.lock.lock_id/entries[0].id,entries[0])
        return dict(model_lock_sha=pool.lock.lock_id,runtime_lock_sha=measure_runtime(),
                    device_profile_sha=measure_device(backend),backend=backend)
    except ModelError as error:raise OperationFailure(str(error)) from error
    except (OSError,ImportError,ValueError,metadata.PackageNotFoundError) as error:
        raise OperationFailure('DEPTH_MEASUREMENT_UNAVAILABLE') from error

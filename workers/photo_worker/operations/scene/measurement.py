"""Installed scene model/runtime identity, separate from measured qualification."""
from hashlib import sha256
from importlib import metadata
from pathlib import Path
import platform
from photo_worker.contracts.base import canonical_hash
from photo_worker.models.install import verify_model


def installed_scene_measurements(pool,backend):
    import torch
    from photo_worker.operations.mask.person_measurement import component_paths,measure_device
    paths={key:path for key,path in component_paths().items() if not key.startswith('mask_') and key!='contracts_person'}
    root=Path(__file__).resolve().parents[2]
    for name in ('parse','measurement'):
        paths['scene_'+name]=root/'operations/scene'/(name+'.py')
    paths['scene_startup']=root/'runtime/scene_config.py'
    paths['scene_authority']=root/'operations/depth/authority.py'
    paths['contracts_semantic']=root/'contracts/semantic.py'
    from photo_worker.models.processing_inventory import processing_component_paths
    paths.update(processing_component_paths())
    components={}
    for name,path in paths.items():
        digest=sha256()
        with path.open('rb') as source:
            for block in iter(lambda:source.read(65536),b''):digest.update(block)
        components[name]=digest.hexdigest()
    entries=[entry for entry in pool.lock.models if entry.id=='segformer_b0_ade']
    if len(entries)!=1:raise ValueError('SCENE_MODEL_UNAVAILABLE')
    verify_model(pool.root/pool.lock.lock_id/entries[0].id,entries[0])
    runtime=canonical_hash(dict(schema_version=1,operation='scene.parse',python_version=platform.python_version(),
        implementation=platform.python_implementation(),machine=platform.machine(),
        packages={name:metadata.version(name) for name in ('numpy','pydantic','rfc8785','torch','torchvision','transformers','Pillow')},
        torch_build=sha256(torch.__config__.show().encode()).hexdigest(),components=components))
    return dict(model_lock_sha=pool.lock.lock_id,runtime_lock_sha=runtime,
        device_profile_sha=measure_device(backend),backend=backend)

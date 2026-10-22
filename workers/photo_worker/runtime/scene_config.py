"""Explicit pinned scene installation; no fabricated passing report."""
import os
from photo_worker.v2.semantic_qualification_pin import _sha


def configured_scene_runtime(config,pool,getenv=os.environ.get):
    enabled=getenv('PHOTO_SCENE_RUNTIME_ENABLED','false')
    names=('MODEL_LOCK_SHA','RUNTIME_LOCK_SHA','DEVICE_PROFILE_SHA')
    values=tuple(getenv('PHOTO_SCENE_'+name,'') for name in names)
    if enabled not in ('true','false') or enabled=='false' and any(values):
        raise ValueError('SCENE_RUNTIME_CONFIGURATION_INVALID')
    if enabled=='false':return None
    if config.resource_class!='gpu' or config.backend not in ('cpu','cuda','mps') or any(not _sha(v) for v in values):
        raise ValueError('SCENE_RUNTIME_CONFIGURATION_INVALID')
    from photo_worker.operations.scene.measurement import installed_scene_measurements
    from photo_worker.operations.depth.authority import depth_runtime_authority
    expected=dict(zip(('model_lock_sha','runtime_lock_sha','device_profile_sha'),values),backend=config.backend)
    measure=lambda:installed_scene_measurements(pool,config.backend)
    if measure()!=expected:raise ValueError('SCENE_RUNTIME_INSTALLATION_MISMATCH')
    return depth_runtime_authority(measure,**expected)

"""Explicit ordinary semantic-transfer installation; measured evidence stays SQL-owned."""
import os
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes
from photo_worker.operations.semantic.scene_runtime import SemanticSceneRuntime,verify_scene_runtime
from photo_worker.v2.semantic_qualification_pin import _sha


def scene_operator_configuration(getenv=os.environ.get):
    enabled=getenv('PHOTO_SEMANTIC_TRANSFER_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('SEMANTIC_ENABLEMENT_INVALID')
    if enabled=='false':return None
    measurements={name:getenv('PHOTO_SEMANTIC_TRANSFER_'+name.upper(),'') for name in
        ('model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha')}
    if any(not _sha(value) for value in measurements.values()):
        raise ValueError('SEMANTIC_MEASUREMENTS_REQUIRED')
    path=Path(getenv('PHOTO_SEMANTIC_TRANSFER_RUNTIME_LOCK',''))
    if not path.is_absolute() or path.is_symlink():raise ValueError('SEMANTIC_RUNTIME_PATH_INVALID')
    with path.open('rb') as source:raw=source.read(1048577)
    if not 2<=len(raw)<=1048576:raise ValueError('SEMANTIC_RUNTIME_SIZE_INVALID')
    lock=SemanticSceneRuntime.model_validate_json(raw)
    # Own canonical bytes rather than keeping an operator-mutable dictionary.
    frozen=canonical_bytes(lock)
    def verify(active):
        return verify_scene_runtime(frozen,measurements['runtime_lock_sha'],check_active=active)
    verify(lambda:None)
    return measurements,verify


def semantic_scene_from_environment(config,getenv=os.environ.get):
    enabled=getenv('PHOTO_SEMANTIC_TRANSFER_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('SEMANTIC_ENABLEMENT_INVALID')
    if enabled=='false':return None
    # Do not read CUDA installation files on sibling workers.
    if config.backend!='cuda' or config.resource_class!='gpu':return None
    configured=scene_operator_configuration(getenv)
    if configured is None:return None
    measurements,verify=configured
    if config.backend!='cuda' or config.resource_class!='gpu':
        raise ValueError('SEMANTIC_TRANSFER_REQUIRES_QUALIFIED_CUDA')
    # Reuse the established bounded object-store transport, not restore authority.
    from .semantic_scene_objects import ordinary_scene_objects
    objects,bucket=ordinary_scene_objects(config.development,getenv)
    from photo_worker.models.lock import ModelLock
    if config.model_lock is None or config.model_root is None or config.model_lock.is_symlink():
        raise ValueError('SEMANTIC_MODEL_LOCK_REQUIRED')
    with config.model_lock.open('rb') as source:raw=source.read(8388609)
    if not 2<=len(raw)<=8388608 or ModelLock.model_validate_json(raw).lock_id!=measurements['model_lock_sha']:
        raise ValueError('SEMANTIC_MODEL_LOCK_MISMATCH')
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.operations.semantic.color_authority import semantic_working_color_authority
    registry=ICCRegistry(config.icc_root)
    color=semantic_working_color_authority(registry.resolve('work_rec2020_gamma24_v1').sha256)
    from .database import TaskDatabase
    from photo_worker.operations.semantic.scene_worker import semantic_scene_worker_factory
    return semantic_scene_worker_factory(database=TaskDatabase(config.database_url),objects=objects,bucket=bucket,
        measurements=measurements,verify_color=color,verify_installed_runtime=verify)

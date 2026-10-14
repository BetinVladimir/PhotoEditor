"""Explicit person-runtime startup configuration, with no self-qualification."""
from photo_worker.v2.semantic_qualification_pin import _sha


def person_runtime_locks_from_env(env,resource_class,backend,models_configured):
    from .service import WorkerConfigurationError
    enabled=env.get('PHOTO_PERSON_RUNTIME_ENABLED','false')
    names=('PHOTO_PERSON_MODEL_LOCK_SHA','PHOTO_PERSON_RUNTIME_LOCK_SHA','PHOTO_PERSON_DEVICE_PROFILE_SHA')
    if enabled not in ('true','false'):raise WorkerConfigurationError('Invalid person runtime enablement')
    if enabled=='false':
        if any(env.get(name) for name in names):raise WorkerConfigurationError('Person runtime locks require enablement')
        return None
    locks=tuple(env.get(name,'') for name in names)
    if resource_class!='gpu' or backend not in ('cpu','cuda','mps') or not models_configured or any(not _sha(value) for value in locks):
        raise WorkerConfigurationError('Person runtime requires GPU-class model worker and complete locks')
    return locks


def configured_person_runtime(config,pool):
    from .service import WorkerConfigurationError
    from photo_worker.operations.mask.person_authority import person_runtime_authority
    from photo_worker.operations.mask.person_measurement import installed_person_measurements
    from .executor import OperationFailure
    if config.person_runtime_locks is None:return None
    try:
        model,runtime,device=config.person_runtime_locks
        expected=dict(model_lock_sha=model,runtime_lock_sha=runtime,device_profile_sha=device,backend=config.backend)
        measure=lambda:installed_person_measurements(pool,config.backend)
        authority=person_runtime_authority(measure,**expected)
        if measure()!=expected:raise OperationFailure('PERSON_RUNTIME_UNQUALIFIED')
        return authority
    except (ValueError,OSError,ImportError,OperationFailure):
        raise WorkerConfigurationError('Person runtime measurement does not match deployment locks') from None

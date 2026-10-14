"""Deployment-owned identity runtime pins and installed measurement."""
from photo_worker.v2.semantic_qualification_pin import _sha


def identity_runtime_locks_from_env(env, resource_class, backend, models_configured):
    from .service import WorkerConfigurationError

    enabled = env.get('PHOTO_IDENTITY_RUNTIME_ENABLED', 'false')
    names = ('PHOTO_IDENTITY_MODEL_LOCK_SHA', 'PHOTO_IDENTITY_RUNTIME_LOCK_SHA',
             'PHOTO_IDENTITY_DEVICE_PROFILE_SHA')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid identity runtime enablement')
    if enabled == 'false':
        if any(env.get(name) for name in names):
            raise WorkerConfigurationError('Identity runtime pins require enablement')
        return None
    locks = tuple(env.get(name, '') for name in names)
    if (resource_class != 'gpu' or backend not in ('cpu', 'cuda') or
            not models_configured or any(not _sha(value) for value in locks)):
        raise WorkerConfigurationError('Identity runtime requires GPU-class model worker and complete locks')
    return locks


def configured_identity_runtime(config, pool):
    from .service import WorkerConfigurationError
    from photo_worker.operations.identity.authority import identity_runtime_authority
    from photo_worker.operations.identity.measurement import installed_identity_measurements
    from .executor import OperationFailure

    if config.identity_runtime_locks is None:
        return None
    try:
        model, runtime, device = config.identity_runtime_locks
        expected = dict(model_lock_sha=model, runtime_lock_sha=runtime,
                        device_profile_sha=device, backend=config.backend)
        measure = lambda: installed_identity_measurements(pool, config.backend)
        authority = identity_runtime_authority(measure, **expected)
        if measure() != expected:
            raise OperationFailure('IDENTITY_RUNTIME_UNQUALIFIED')
        return authority
    except (ValueError, OSError, ImportError, OperationFailure):
        raise WorkerConfigurationError('Identity runtime measurement does not match deployment locks') from None

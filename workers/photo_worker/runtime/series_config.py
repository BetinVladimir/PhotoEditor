"""Deployment pins for qualified CPU series.balance execution."""
import re


def series_runtime_locks_from_env(env, resource_class, backend):
    from .service import WorkerConfigurationError
    names = ('PHOTO_SERIES_MODEL_LOCK_SHA', 'PHOTO_SERIES_RUNTIME_LOCK_SHA',
             'PHOTO_SERIES_DEVICE_PROFILE_SHA')
    enabled = env.get('PHOTO_SERIES_RUNTIME_ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid series runtime enablement')
    if enabled == 'false':
        if any(env.get(name) for name in names):
            raise WorkerConfigurationError('Series runtime pins require enablement')
        return None
    locks = tuple(env.get(name, '') for name in names)
    if (resource_class != 'cpu' or backend != 'cpu' or
            any(re.fullmatch(r'[0-9a-f]{64}', value) is None for value in locks)):
        raise WorkerConfigurationError('Series runtime requires CPU worker and complete pins')
    return locks

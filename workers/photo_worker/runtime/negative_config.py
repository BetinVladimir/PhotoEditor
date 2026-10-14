"""Fail-closed operator configuration for the V29 negative CPU worker."""
import os
import re
from pathlib import Path
from .service import WorkerConfigurationError
from photo_worker.operations.optical.negative_adapter import NegativeRuntimeOptions
from photo_worker.operations.optical.negative_runtime_lock import load_negative_runtime_lock


def negative_options_from_environment(config, getenv=os.environ.get):
    enabled = getenv('PHOTO_NEGATIVE_ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid negative enablement')
    if enabled == 'false':
        return None
    if config.resource_class != 'cpu' or config.backend != 'cpu':
        raise WorkerConfigurationError('Negative runtime requires CPU worker')
    total = config.max_scratch_bytes
    if type(total) is not int or not 1 <= total <= 2**53-1:
        raise WorkerConfigurationError('Invalid negative scratch budget')
    digest = getenv('PHOTO_NEGATIVE_RUNTIME_SHA', '')
    path = getenv('PHOTO_NEGATIVE_RUNTIME_LOCK', '')
    if (not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest)
            or not isinstance(path, str) or not path or not Path(path).is_absolute()):
        raise WorkerConfigurationError('Invalid negative runtime pin')
    limits = []
    for name in ('SOURCE', 'RAM'):
        value = getenv('PHOTO_NEGATIVE_' + name + '_MAX_BYTES', '')
        if not isinstance(value, str) or not re.fullmatch('[1-9][0-9]{0,15}', value):
            raise WorkerConfigurationError('Invalid negative resource budget')
        number = int(value)
        if number > 2**53-1:
            raise WorkerConfigurationError('Invalid negative resource budget')
        limits.append(number)
    if limits[0] >= total:
        raise WorkerConfigurationError('Negative source exhausts scratch budget')
    try:
        lock = load_negative_runtime_lock(Path(path), digest)
    except (OSError, ValueError, RuntimeError):
        raise WorkerConfigurationError('Unqualified negative runtime lock') from None
    return NegativeRuntimeOptions(lock, digest, *limits)


def negative_installation_from_environment(config, getenv=os.environ.get):
    options = negative_options_from_environment(config, getenv)
    if options is None:
        return None
    from photo_worker.color.icc import ICCRegistry
    from photo_worker.operations.optical.negative_color import negative_icc_verifier
    from photo_worker.operations.optical.negative_adapter import NegativeInstallation
    from .donor_config import qualified_object_storage_from_environment
    try:
        if config.icc_root is None:
            raise ValueError('Missing ICC registry')
        negative_icc_verifier(ICCRegistry(config.icc_root))
    except (OSError, ValueError, KeyError, RuntimeError):
        raise WorkerConfigurationError('Unqualified negative ICC registry') from None
    objects, bucket = qualified_object_storage_from_environment(config, getenv)
    return NegativeInstallation(objects, bucket, options)

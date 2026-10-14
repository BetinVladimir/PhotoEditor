"""Bounded SHA-pinned automatic-line deployment configuration; default off."""
from hashlib import sha256
import os
from pathlib import Path
import re
import stat

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.optical.perspective_lines_installation import PerspectiveLinesInstallation, execution_policy
from photo_worker.operations.optical.perspective_lines_runtime_lock import PerspectiveLinesRuntimeLock, verify_installed_runtime
from .service import WorkerConfigurationError

MAX_CONFIG_BYTES = 8 << 20


def load_perspective_lines_installation(path, digest):
    if (not isinstance(path, (str, Path)) or not Path(path).is_absolute()
            or not isinstance(digest, str) or not re.fullmatch('[0-9a-f]{64}', digest)):
        raise ValueError('PERSPECTIVE_LINES_CONFIG_PIN_INVALID')
    descriptor = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(descriptor, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or not 2 <= info.st_size <= MAX_CONFIG_BYTES:
            raise ValueError('PERSPECTIVE_LINES_CONFIG_FILE_INVALID')
        raw = stream.read(MAX_CONFIG_BYTES + 1)
    if len(raw) != info.st_size or sha256(raw).hexdigest() != digest:
        raise ValueError('PERSPECTIVE_LINES_CONFIG_BYTES_MISMATCH')
    value = parse_json(raw, MAX_CONFIG_BYTES)
    if (not isinstance(value, dict) or set(value) != {'schema_version', 'runtime_lock', 'runtime_sha', 'working_icc_sha'}
            or type(value['schema_version']) is not int or value['schema_version'] != 1
            or canonical_bytes(value) != raw):
        raise ValueError('PERSPECTIVE_LINES_CONFIG_INVALID')
    execution_policy(value['working_icc_sha'], value['runtime_sha'])
    lock = PerspectiveLinesRuntimeLock.model_validate_json(canonical_bytes(value['runtime_lock']))
    verify_installed_runtime(lock, value['runtime_sha'])
    return PerspectiveLinesInstallation(lock, value['runtime_sha'], value['working_icc_sha'])


def perspective_lines_installation_from_environment(config, getenv=os.environ.get):
    enabled = getenv('PHOTO_PERSPECTIVE_LINES_ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid perspective lines enablement')
    if enabled == 'false':
        return None
    if config.resource_class != 'cpu' or config.backend != 'cpu' or config.icc_root is None:
        raise WorkerConfigurationError('Perspective lines requires CPU worker and ICC registry')
    try:
        installation = load_perspective_lines_installation(
            getenv('PHOTO_PERSPECTIVE_LINES_CONFIG', ''), getenv('PHOTO_PERSPECTIVE_LINES_CONFIG_SHA', ''))
        from photo_worker.color.icc import ICCRegistry
        if ICCRegistry(config.icc_root).resolve('linear_srgb_v1').sha256 != installation.working_icc_sha:
            raise ValueError('PERSPECTIVE_LINES_ICC_MISMATCH')
        return installation
    except (OSError, ValueError, RuntimeError, KeyError, TypeError):
        raise WorkerConfigurationError('Invalid perspective lines runtime or ICC configuration') from None

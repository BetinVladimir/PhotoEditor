"""Explicit V28 hair/clothing CPU installation with a pinned runtime lock."""
from dataclasses import dataclass
from pathlib import Path
import os
import re

from .service import WorkerConfigurationError
from photo_worker.operations.portrait.local_runtime_lock import load_local_portrait_runtime_lock


@dataclass(frozen=True)
class LocalPortraitInstallation:
    runtime_lock: object
    runtime_sha: str


def local_portrait_installation_from_environment(config, getenv=os.environ.get):
    enabled = getenv('PHOTO_LOCAL_PORTRAIT_ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid local portrait enablement')
    if enabled == 'false':
        return None
    if config.resource_class != 'cpu' or config.backend != 'cpu':
        raise WorkerConfigurationError('Local portrait requires CPU worker')
    path = getenv('PHOTO_LOCAL_PORTRAIT_RUNTIME_LOCK', '')
    digest = getenv('PHOTO_LOCAL_PORTRAIT_RUNTIME_SHA', '')
    if (not isinstance(path, str) or not path or not Path(path).is_absolute()
            or not isinstance(digest, str) or not re.fullmatch(r'[0-9a-f]{64}', digest)):
        raise WorkerConfigurationError('Invalid local portrait runtime pin')
    try:
        lock = load_local_portrait_runtime_lock(Path(path), digest)
    except (OSError, ValueError, RuntimeError):
        raise WorkerConfigurationError('Unqualified local portrait runtime lock') from None
    return LocalPortraitInstallation(lock, digest)

"""Explicit operator installation for the qualified V28 reflection model."""

from dataclasses import dataclass
import os
from pathlib import Path
import re

from photo_worker.models.install import ModelError, verify_model
from photo_worker.models.lock import ModelLock
from photo_worker.operations.image.reflection_runtime_lock import (
    ReflectionRuntimeLock, load_reflection_runtime_lock,
)
from photo_worker.runtime.service import WorkerConfigurationError


@dataclass(frozen=True)
class ReflectionInstallation:
    model_lock_sha: str
    runtime_lock: ReflectionRuntimeLock
    runtime_sha: str


def reflection_installation_from_environment(config, getenv=os.environ.get):
    enabled = getenv('PHOTO_REFLECTION_ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid reflection enablement')
    if enabled == 'false':
        return None
    if (config.resource_class != 'gpu' or config.backend not in ('cpu', 'cuda')
            or config.model_root is None or config.model_lock is None):
        raise WorkerConfigurationError('Reflection requires a locked CPU/CUDA GPU-class worker')
    model_sha = getenv('PHOTO_REFLECTION_MODEL_LOCK_SHA', '')
    runtime_sha = getenv('PHOTO_REFLECTION_RUNTIME_SHA', '')
    runtime_path = getenv('PHOTO_REFLECTION_RUNTIME_LOCK', '')
    if (not re.fullmatch(r'[0-9a-f]{64}', model_sha)
            or not re.fullmatch(r'[0-9a-f]{64}', runtime_sha)
            or not runtime_path or not Path(runtime_path).is_absolute()):
        raise WorkerConfigurationError('Invalid reflection installation pins')
    try:
        lock = ModelLock.model_validate_json(config.model_lock.read_bytes())
        if lock.lock_id != model_sha:
            raise ValueError('Model lock mismatch')
        entries = [item for item in lock.models if item.id == 'reflection_rgb']
        if (len(entries) != 1 or not entries[0].approved_for_install
                or entries[0].backends[config.backend].status != 'passed'):
            raise ValueError('Unqualified model')
        verify_model(config.model_root / model_sha / 'reflection_rgb', entries[0])
        runtime_lock = load_reflection_runtime_lock(
            Path(runtime_path), runtime_sha, config.backend)
    except (OSError, ValueError, ModelError, KeyError):
        raise WorkerConfigurationError('Unqualified reflection installation') from None
    return ReflectionInstallation(model_sha, runtime_lock, runtime_sha)

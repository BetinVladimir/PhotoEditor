"""Operator-owned donor glare installation; unavailable unless fully pinned."""

from dataclasses import dataclass
import os
from pathlib import Path
import re

from photo_worker.models.lock import ModelLock
from photo_worker.models.install import ModelError, verify_model
from photo_worker.operations.portrait.glare_donor_runtime_lock import (
    GlareDonorRuntimeLock, load_glare_donor_runtime_lock,
)
from photo_worker.runtime.service import WorkerConfigurationError


@dataclass(frozen=True)
class GlareDonorInstallation:
    objects: object
    bucket: str
    runtime_lock: GlareDonorRuntimeLock
    runtime_sha: str
    model_lock_sha: str
    material_scratch_root: Path
    material_max_bytes: int
    alpha_mode: str


def glare_donor_installation_from_environment(config, getenv=os.environ.get):
    enabled = getenv('PHOTO_GLARE_DONOR_ENABLED', 'false')
    if enabled not in ('true', 'false'):
        raise WorkerConfigurationError('Invalid donor glare enablement')
    if enabled == 'false':
        return None
    if (config.resource_class != 'gpu' or config.backend != 'cpu'
            or config.model_lock is None or config.model_root is None):
        raise WorkerConfigurationError('Donor glare requires locked CPU GPU-class worker')
    runtime_sha = getenv('PHOTO_GLARE_DONOR_RUNTIME_SHA', '')
    model_sha = getenv('PHOTO_GLARE_DONOR_MODEL_LOCK_SHA', '')
    lock_path = getenv('PHOTO_GLARE_DONOR_RUNTIME_LOCK', '')
    root_value = getenv('PHOTO_GLARE_DONOR_MATERIAL_SCRATCH_ROOT', '')
    limit_value = getenv('PHOTO_GLARE_DONOR_MATERIAL_MAX_BYTES', '')
    alpha = getenv('PHOTO_GLARE_DONOR_ALPHA_MODE', 'require_float32')
    if (not re.fullmatch(r'[0-9a-f]{64}', runtime_sha)
            or not re.fullmatch(r'[0-9a-f]{64}', model_sha)
            or not lock_path or not Path(lock_path).is_absolute()
            or not root_value or not Path(root_value).is_absolute()
            or not re.fullmatch(r'[1-9][0-9]{0,15}', limit_value)
            or alpha not in ('require_float32', 'normalize_uint16')):
        raise WorkerConfigurationError('Invalid donor glare pins or scratch configuration')
    material_root = Path(root_value)
    limit = int(limit_value)
    if (material_root.is_symlink() or not material_root.is_dir()
            or limit > config.max_scratch_bytes or limit > 2**53-1):
        raise WorkerConfigurationError('Invalid donor glare scratch budget')
    try:
        model_lock = ModelLock.model_validate_json(config.model_lock.read_bytes())
        if model_lock.lock_id != model_sha:
            raise ValueError('Model lock mismatch')
        entries = [item for item in model_lock.models if item.id == 'glare_mask']
        if (len(entries) != 1 or not entries[0].approved_for_install
                or entries[0].backends['cpu'].status != 'passed'):
            raise ValueError('Unqualified glare mask')
        verify_model(config.model_root / model_sha / 'glare_mask', entries[0])
        runtime_lock = load_glare_donor_runtime_lock(Path(lock_path), runtime_sha)
        from photo_worker.runtime.donor_config import qualified_object_storage_from_environment
        objects, bucket = qualified_object_storage_from_environment(config, getenv)
    except (OSError, ValueError, ModelError, KeyError):
        raise WorkerConfigurationError('Unqualified donor glare installation') from None
    return GlareDonorInstallation(objects, bucket, runtime_lock, runtime_sha,
                                  model_sha, material_root, limit, alpha)

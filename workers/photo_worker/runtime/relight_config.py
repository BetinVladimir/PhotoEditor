"""Explicit qualified CPU relight worker installation; disabled by default."""
from dataclasses import dataclass
from pathlib import Path
import os
import re
from .service import WorkerConfigurationError
from photo_worker.operations.portrait.relight_runtime_lock import load_relight_runtime_lock


@dataclass(frozen=True)
class RelightInstallation:
    objects: object
    bucket: str
    runtime_lock: object
    runtime_sha: str


def relight_installation_from_environment(config,getenv=os.environ.get):
    enabled=getenv('PHOTO_RELIGHT_ENABLED','false')
    if enabled not in ('true','false'):raise WorkerConfigurationError('Invalid relight enablement')
    if enabled=='false':return None
    if config.resource_class!='cpu' or config.backend!='cpu':
        raise WorkerConfigurationError('Relight requires CPU worker')
    path=getenv('PHOTO_RELIGHT_RUNTIME_LOCK','');digest=getenv('PHOTO_RELIGHT_RUNTIME_SHA','')
    if (not isinstance(path,str) or not path or not Path(path).is_absolute()
            or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest)):
        raise WorkerConfigurationError('Invalid relight runtime pin')
    try:lock=load_relight_runtime_lock(Path(path),digest)
    except (OSError,ValueError,RuntimeError):
        raise WorkerConfigurationError('Unqualified relight runtime lock') from None
    from .donor_config import qualified_object_storage_from_environment
    objects,bucket=qualified_object_storage_from_environment(config,getenv)
    return RelightInstallation(objects,bucket,lock,digest)

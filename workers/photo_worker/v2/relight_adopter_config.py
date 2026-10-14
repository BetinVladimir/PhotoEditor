"""Explicit operator-pinned relight adopter installation; disabled by default."""
import os
import re
from pathlib import Path
from .relight_adoption_prepare import RelightAdoptionInstallation
from photo_worker.operations.portrait.relight_runtime_lock import load_relight_runtime_lock


def relight_adopter_from_environment(getenv=os.environ.get):
    enabled=getenv('PHOTO_ADOPTER_RELIGHT_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('Invalid relight adopter enablement')
    if enabled=='false':return None
    root=Path(getenv('PHOTO_ADOPTER_RELIGHT_SCRATCH_ROOT',''))
    if not root.is_absolute() or root.is_symlink() or not root.is_dir() or root.resolve()!=root:
        raise ValueError('Invalid relight adopter scratch root')
    def budget(name):
        value=getenv(name,'')
        if not isinstance(value,str) or not re.fullmatch('[1-9][0-9]{0,15}',value) or int(value)>2**53-1:
            raise ValueError('Invalid relight adopter scratch budget')
        return int(value)
    maximum=budget('PHOTO_ADOPTER_RELIGHT_INPUT_BYTES')
    total=budget('PHOTO_ADOPTER_RELIGHT_TOTAL_SCRATCH_BYTES')
    if maximum>total:raise ValueError('Invalid relight adopter aggregate budget')
    path=getenv('PHOTO_ADOPTER_RELIGHT_RUNTIME_LOCK','')
    digest=getenv('PHOTO_ADOPTER_RELIGHT_RUNTIME_SHA','')
    if (not isinstance(path,str) or not path or not Path(path).is_absolute()
            or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest)):
        raise ValueError('Invalid relight adopter runtime pin')
    lock=load_relight_runtime_lock(path,digest)
    return RelightAdoptionInstallation(lock,digest,root,maximum,total)

"""Explicit independent detector adopter runtime and resource pins."""
import os
from pathlib import Path
import re
from photo_worker.color.icc import ICCRegistry
from photo_worker.runtime.perspective_lines_config import load_perspective_lines_installation
from .perspective_lines_adoption_prepare import PerspectiveLinesAdoptionInstallation, validate_perspective_lines_adoption_installation


def perspective_lines_adopter_from_environment(getenv=os.environ.get):
    enabled=getenv('PHOTO_ADOPTER_PERSPECTIVE_LINES_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('Invalid perspective lines adopter enablement')
    if enabled=='false':return None
    budgets=[]
    for resource in ('SCRATCH','RAM'):
        raw=getenv('PHOTO_ADOPTER_PERSPECTIVE_LINES_'+resource+'_MAX_BYTES','')
        if not isinstance(raw,str) or not re.fullmatch('[1-9][0-9]{0,15}',raw) or int(raw)>2**53-1:
            raise ValueError('Invalid perspective lines adopter resource limit')
        budgets.append(int(raw))
    root=Path(getenv('PHOTO_ADOPTER_PERSPECTIVE_LINES_SCRATCH_ROOT',''))
    icc=Path(getenv('PHOTO_ICC_ROOT',''))
    for path in (root,icc):
        if not path.is_absolute() or path.is_symlink() or not path.is_dir() or path.resolve()!=path:
            raise ValueError('Invalid perspective lines adopter directory')
    runtime=load_perspective_lines_installation(getenv('PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG',''),
        getenv('PHOTO_ADOPTER_PERSPECTIVE_LINES_CONFIG_SHA',''))
    if ICCRegistry(icc).resolve('linear_srgb_v1').sha256!=runtime.working_icc_sha:
        raise ValueError('Perspective lines adopter ICC mismatch')
    return validate_perspective_lines_adoption_installation(PerspectiveLinesAdoptionInstallation(runtime,root,*budgets))

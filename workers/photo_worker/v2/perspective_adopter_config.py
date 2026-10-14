"""Explicit perspective adopter pins and bounded replay resources."""
import os
import re
from pathlib import Path
from photo_worker.runtime.perspective_config import load_perspective_options
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.optical.perspective_color import perspective_icc_verifier
from .perspective_adoption_prepare import PerspectiveAdoptionInstallation,validate_perspective_adoption_installation


def perspective_adopter_from_environment(getenv=os.environ.get):
    enabled=getenv('PHOTO_ADOPTER_PERSPECTIVE_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('Invalid perspective adopter enablement')
    if enabled=='false':return None
    budgets=[]
    for name in ('SCRATCH','RAM'):
        raw=getenv('PHOTO_ADOPTER_PERSPECTIVE_'+name+'_MAX_BYTES','')
        if not isinstance(raw,str) or not re.fullmatch('[1-9][0-9]{0,15}',raw) or int(raw)>2**53-1:
            raise ValueError('Invalid perspective adopter resource limit')
        budgets.append(int(raw))
    root=Path(getenv('PHOTO_ADOPTER_PERSPECTIVE_SCRATCH_ROOT',''))
    icc=Path(getenv('PHOTO_ICC_ROOT',''))
    for path in (root,icc):
        if not path.is_absolute() or path.is_symlink() or not path.is_dir() or path.resolve()!=path:
            raise ValueError('Invalid perspective adopter directory')
    options=load_perspective_options(getenv('PHOTO_ADOPTER_PERSPECTIVE_CONFIG',''),getenv('PHOTO_ADOPTER_PERSPECTIVE_CONFIG_SHA',''))
    verify_color=perspective_icc_verifier(ICCRegistry(icc))
    return validate_perspective_adoption_installation(PerspectiveAdoptionInstallation(options,root,*budgets,verify_color))

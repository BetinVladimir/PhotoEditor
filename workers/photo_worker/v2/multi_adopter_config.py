"""Explicit multi-image adopter pins and bounded replay resources."""
import os
import re
from pathlib import Path
from photo_worker.runtime.multi_config import load_multi_options
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.optical.perspective_color import perspective_icc_verifier
from .multi_adoption_prepare import MultiImageAdoptionInstallation,validate_multi_adoption_installation


def multi_adopter_from_environment(operation,getenv=os.environ.get):
    if operation not in ('optical.panorama','optical.focus_stack'):raise ValueError('Invalid multi adopter operation')
    prefix='PHOTO_ADOPTER_PANORAMA' if operation=='optical.panorama' else 'PHOTO_ADOPTER_FOCUS_STACK'
    enabled=getenv(prefix+'_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('Invalid multi adopter enablement')
    if enabled=='false':return None
    budgets=[]
    for name in ('SCRATCH','RAM'):
        raw=getenv(prefix+'_'+name+'_MAX_BYTES','')
        if not isinstance(raw,str) or not re.fullmatch('[1-9][0-9]{0,15}',raw) or int(raw)>2**53-1:
            raise ValueError('Invalid multi adopter resource limit')
        budgets.append(int(raw))
    root=Path(getenv(prefix+'_SCRATCH_ROOT',''))
    icc=Path(getenv('PHOTO_ICC_ROOT',''))
    for path in (root,icc):
        if not path.is_absolute() or path.is_symlink() or not path.is_dir() or path.resolve()!=path:
            raise ValueError('Invalid multi adopter directory')
    options=load_multi_options(getenv(prefix+'_CONFIG',''),getenv(prefix+'_CONFIG_SHA',''))
    if options.operation!=operation:raise ValueError('Multi adopter operation mismatch')
    verify_color=perspective_icc_verifier(ICCRegistry(icc))
    return validate_multi_adoption_installation(MultiImageAdoptionInstallation(options,root,*budgets,verify_color))

"""Pinned operator-only negative adopter installation."""
import os
import re
import stat
from pathlib import Path
from types import SimpleNamespace
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime.negative_config import negative_options_from_environment
from photo_worker.color.icc import ICCRegistry
from photo_worker.operations.optical.negative_color import negative_icc_verifier
from .negative_adoption_prepare import NegativeAdoptionInstallation


def negative_adopter_from_environment(getenv=os.environ.get):
    enabled=getenv('PHOTO_ADOPTER_NEGATIVE_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('Invalid negative adopter enablement')
    if enabled=='false':return None
    raw=getenv('PHOTO_ADOPTER_NEGATIVE_TOTAL_SCRATCH_BYTES','')
    if not isinstance(raw,str) or not re.fullmatch('[1-9][0-9]{0,15}',raw) or int(raw)>2**53-1:
        raise ValueError('Invalid negative adopter scratch budget')
    total=int(raw)
    root=Path(getenv('PHOTO_ADOPTER_NEGATIVE_SCRATCH_ROOT',''))
    if not root.is_absolute() or root.is_symlink() or not root.is_dir() or root.resolve()!=root:
        raise ValueError('Invalid negative adopter scratch root')
    options=negative_options_from_environment(SimpleNamespace(resource_class='cpu',backend='cpu',max_scratch_bytes=total),
        lambda key,default=None:'true' if key=='PHOTO_NEGATIVE_ENABLED' else getenv(key,default))
    path=getenv('PHOTO_ADOPTER_NEGATIVE_POLICY','');digest=getenv('PHOTO_ADOPTER_NEGATIVE_POLICY_SHA','')
    if not isinstance(path,str) or not path or not Path(path).is_absolute() or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest):raise ValueError('Invalid negative adopter policy pin')
    try:
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as stream:
            info=os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 2<=info.st_size<=1<<20:raise ValueError()
            raw=stream.read((1<<20)+1)
        policy=parse_json(raw,1<<20)
        if (len(raw)!=info.st_size or sha256(raw).hexdigest()!=digest or canonical_bytes(policy)!=raw
                or not isinstance(policy,dict) or policy.get('operation')!='optical.negative'
                or policy.get('operation_version')!='1' or policy.get('backend')!='cpu'
                or policy.get('runtime_lock_sha')!=options.runtime_sha):raise ValueError()
    except (OSError,ValueError):raise ValueError('Invalid negative adopter policy file') from None

    icc=getenv('PHOTO_ICC_ROOT','')
    if not icc or not Path(icc).is_absolute() or Path(icc).resolve()!=Path(icc):
        raise ValueError('Invalid negative adopter ICC root')
    verify_color=negative_icc_verifier(ICCRegistry(Path(icc)))
    return NegativeAdoptionInstallation(policy,options,root,total,verify_color)

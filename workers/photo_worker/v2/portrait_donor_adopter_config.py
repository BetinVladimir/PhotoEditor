"""Explicit pinned donor adopter configuration; no client-controlled providers."""
import os
import re
import stat
from pathlib import Path
from types import SimpleNamespace
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes,parse_json
from photo_worker.runtime.donor_config import donor_options_from_environment
from .portrait_donor_adoption_prepare import DonorAdoptionInstallation


def donor_adopter_from_environment(getenv=os.environ.get):
    enabled=getenv('PHOTO_ADOPTER_DONOR_ENABLED','false')
    if enabled not in ('true','false'):raise ValueError('Invalid donor adopter enablement')
    if enabled=='false':return None
    def number(key):
        raw=getenv(key,'')
        if not re.fullmatch('[1-9][0-9]{0,15}',raw) or int(raw)>2**53-1:
            raise ValueError('Invalid donor adopter budget')
        return int(raw)
    ram=number('PHOTO_ADOPTER_DONOR_RAM_MAX_BYTES')
    total=number('PHOTO_ADOPTER_DONOR_TOTAL_SCRATCH_BYTES')
    output=number('PHOTO_ADOPTER_DONOR_OUTPUT_MAX_BYTES')
    root=Path(getenv('PHOTO_ADOPTER_DONOR_OUTPUT_SCRATCH_ROOT',''))
    if not root.is_absolute() or root.is_symlink() or not root.is_dir():
        raise ValueError('Invalid donor adopter scratch root')
    options=donor_options_from_environment(SimpleNamespace(resource_class='cpu',backend='cpu',max_scratch_bytes=total),
        lambda key,default=None:'true' if key=='PHOTO_DONOR_ENABLED' else getenv(key,default))
    if options.mask_max_bytes+options.material_max_bytes+output>total:
        raise ValueError('Donor adopter aggregate scratch budget exceeded')
    path=getenv('PHOTO_ADOPTER_DONOR_POLICY','');digest=getenv('PHOTO_ADOPTER_DONOR_POLICY_SHA','')
    if not path or not re.fullmatch('[0-9a-f]{64}',digest):raise ValueError('Invalid donor adopter policy pin')
    try:
        fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
        with os.fdopen(fd,'rb') as stream:
            info=os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 2<=info.st_size<=1<<20:raise ValueError()
            raw=stream.read((1<<20)+1)
        policy=parse_json(raw,1<<20)
        if (len(raw)!=info.st_size or sha256(raw).hexdigest()!=digest or canonical_bytes(policy)!=raw
                or not isinstance(policy,dict) or policy.get('operation')!='portrait.open_eyes'
                or policy.get('operation_version')!='1' or policy.get('backend')!='cpu'
                or policy.get('runtime_lock_sha')!=options.runtime_sha):raise ValueError()
    except (OSError,ValueError):raise ValueError('Invalid donor adopter policy file') from None
    return DonorAdoptionInstallation(policy,options,root,output,ram)

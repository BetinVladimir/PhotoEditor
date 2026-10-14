"""Bounded operator configuration for the qualified V28 donor CPU runtime."""
import os
import re
from pathlib import Path
from .service import WorkerConfigurationError
from photo_worker.operations.portrait.donor_adapter import DonorRuntimeOptions
from photo_worker.operations.portrait.donor_runtime_lock import load_donor_portrait_runtime_lock


def donor_options_from_environment(config,getenv=os.environ.get):
    enabled=getenv('PHOTO_DONOR_ENABLED','false')
    if enabled not in ('true','false'):raise WorkerConfigurationError('Invalid donor enablement')
    if enabled=='false':return None
    if config.resource_class!='cpu' or config.backend!='cpu':
        raise WorkerConfigurationError('Donor runtime requires CPU worker')
    if type(config.max_scratch_bytes) is not int or not 1<=config.max_scratch_bytes<=2**53-1:
        raise WorkerConfigurationError('Invalid donor total scratch budget')
    digest=getenv('PHOTO_DONOR_RUNTIME_SHA','')
    lock_path=getenv('PHOTO_DONOR_RUNTIME_LOCK','')
    alpha=getenv('PHOTO_DONOR_ALPHA_MODE','require_float32')
    if not re.fullmatch('[0-9a-f]{64}',digest) or not lock_path or alpha not in ('require_float32','normalize_uint16'):
        raise WorkerConfigurationError('Invalid donor runtime pin')
    roots=[];limits=[]
    for name in ('MASK','MATERIAL'):
        value=getenv('PHOTO_DONOR_'+name+'_SCRATCH_ROOT','')
        number=getenv('PHOTO_DONOR_'+name+'_MAX_BYTES','')
        if not value or not Path(value).is_absolute() or not re.fullmatch('[1-9][0-9]{0,15}',number):
            raise WorkerConfigurationError('Invalid donor scratch configuration')
        root=Path(value);limit=int(number)
        if root.is_symlink() or not root.is_dir() or limit>config.max_scratch_bytes or limit>2**53-1:
            raise WorkerConfigurationError('Invalid donor scratch configuration')
        roots.append(root);limits.append(limit)
    if sum(limits)>config.max_scratch_bytes:
        raise WorkerConfigurationError('Donor aggregate scratch budget exceeded')
    try:lock=load_donor_portrait_runtime_lock(Path(lock_path),digest)
    except (OSError,ValueError,RuntimeError):
        raise WorkerConfigurationError('Unqualified donor runtime lock') from None
    return DonorRuntimeOptions(lock,digest,*roots,*limits,alpha)


def donor_installation_from_environment(config,getenv=os.environ.get):
    options=donor_options_from_environment(config,getenv)
    if options is None:return None
    from photo_worker.operations.portrait.donor_adapter import DonorInstallation
    objects,bucket=qualified_object_storage_from_environment(config,getenv)
    return DonorInstallation(objects,bucket,options)


def qualified_object_storage_from_environment(config,getenv=os.environ.get):
    from urllib.parse import urlsplit
    endpoint=getenv('S3_ENDPOINT','');bucket=getenv('S3_BUCKET','')
    access=getenv('S3_ACCESS_KEY','');secret=getenv('S3_SECRET_KEY','')
    region=getenv('S3_REGION','')
    try:
        parsed=urlsplit(endpoint)
        if (parsed.scheme not in ('https','http') or not parsed.hostname or parsed.username is not None
                or parsed.password is not None or parsed.path not in ('','/') or parsed.query or parsed.fragment
                or parsed.scheme=='http' and not config.development):raise ValueError()
        parsed.port
        if (not re.fullmatch(r'[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]',bucket)
                or '..' in bucket or not access or not secret):raise ValueError()
    except (ValueError,TypeError):
        raise WorkerConfigurationError('Invalid donor object storage configuration') from None
    from minio import Minio
    import certifi
    import urllib3
    from .semantic_objects import SemanticObjectReader
    http=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5,read=10),retries=False,
        cert_reqs='CERT_REQUIRED',ca_certs=certifi.where())
    objects=Minio(parsed.netloc,access_key=access,secret_key=secret,
        secure=parsed.scheme=='https',region=region or None,http_client=http)
    return SemanticObjectReader(objects),bucket

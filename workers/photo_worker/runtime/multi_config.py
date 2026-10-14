"""SHA-pinned bounded operator multi-image installation; disabled by default."""
import os
import re
import stat
from pathlib import Path
from photo_worker.contracts.base import parse_json,canonical_bytes,canonical_hash
from photo_worker.operations.optical.multi_runtime_lock import MultiImageRuntimeLock,verify_installed_runtime
from photo_worker.operations.optical.multi_installation import MultiImageRuntimeOptions,MultiImageInstallation
from photo_worker.operations.optical.multi_admission import PinnedMultiImageAdmission
from .service import WorkerConfigurationError

MAX_CONFIG_BYTES=8<<20


def load_multi_options(path,digest):
    if not isinstance(path,(str,Path)) or not Path(path).is_absolute() or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest):
        raise ValueError('MULTI_CONFIG_PIN_INVALID')
    descriptor=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_NONBLOCK)
    from hashlib import sha256
    with os.fdopen(descriptor,'rb') as stream:
        info=os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_nlink!=1 or not 2<=info.st_size<=MAX_CONFIG_BYTES:
            raise ValueError('MULTI_CONFIG_FILE_INVALID')
        raw=stream.read(MAX_CONFIG_BYTES+1)
    if len(raw)!=info.st_size or sha256(raw).hexdigest()!=digest:raise ValueError('MULTI_CONFIG_BYTES_MISMATCH')
    value=parse_json(raw,MAX_CONFIG_BYTES)
    if not isinstance(value,dict) or set(value)!={'schema_version','operation','runtime_lock','runtime_sha','pin','policy','max_pixels'} or type(value['schema_version']) is not int or value['schema_version']!=1:
        raise ValueError('MULTI_CONFIG_INVALID')
    lock=MultiImageRuntimeLock.model_validate_json(canonical_bytes(value['runtime_lock']))
    verify_installed_runtime(lock,value['runtime_sha'])
    # Constructor performs identity/limit checks; callbacks are not invoked
    # while reading configuration. Live evidence is mandatory at execution.
    PinnedMultiImageAdmission(operation=value['operation'],runtime_lock=lock,runtime_sha=value['runtime_sha'],pin=value['pin'],policy=value['policy'],
        max_pixels=value['max_pixels'],resolve_evidence=lambda *args:None,verify_color=lambda *args:None)
    policy=value['policy'];pin=value['pin']
    from photo_worker.v2.semantic_qualification_report import POLICY_KEYS
    if (not isinstance(policy,dict) or set(policy)!=POLICY_KEYS or type(policy['schema_version']) is not int or policy['schema_version']!=1 or
        policy.get('operation')!=value['operation'] or policy.get('operation_version')!='1' or
        policy.get('backend')!='cpu' or policy.get('license_cleared') is not True or canonical_hash(policy)!=pin['policy_sha']):
        raise ValueError('MULTI_CONFIG_POLICY_INVALID')
    for name in ('policy_id','model_lock_sha','runtime_lock_sha','device_profile_sha','dataset_sha','issuer_id'):
        if policy[name]!=pin[name]:raise ValueError('MULTI_CONFIG_POLICY_INVALID')
    return MultiImageRuntimeOptions(value['operation'],lock,value['runtime_sha'],value['pin'],policy,value['max_pixels'])


def multi_installation_from_environment(config,operation, getenv=os.environ.get):
    if operation not in ('optical.panorama','optical.focus_stack'):
        raise WorkerConfigurationError('Invalid multi-image operation')
    prefix='PHOTO_PANORAMA' if operation=='optical.panorama' else 'PHOTO_FOCUS_STACK'
    enabled=getenv(prefix+'_ENABLED','false')
    if enabled not in ('true','false'):raise WorkerConfigurationError('Invalid multi-image enablement')
    if enabled=='false':return None
    if config.resource_class!='cpu' or config.backend!='cpu' or config.icc_root is None:
        raise WorkerConfigurationError('Multi-image requires CPU worker and ICC registry')
    try:
        options=load_multi_options(getenv(prefix+'_CONFIG',''),getenv(prefix+'_CONFIG_SHA',''))
        if options.operation!=operation:raise ValueError('MULTI_CONFIG_OPERATION_MISMATCH')
        from photo_worker.color.icc import ICCRegistry
        from photo_worker.operations.optical.perspective_color import perspective_icc_verifier
        perspective_icc_verifier(ICCRegistry(config.icc_root))
    except (OSError,ValueError,RuntimeError,KeyError,TypeError):
        raise WorkerConfigurationError('Invalid or unqualified multi-image installation') from None
    from .donor_config import qualified_object_storage_from_environment
    objects,bucket=qualified_object_storage_from_environment(config,getenv)
    return MultiImageInstallation(objects,bucket,options)

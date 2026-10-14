"""Explicit operator installation for qualified multi CPU execution."""
from dataclasses import dataclass
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.runtime.executor import Operation,OperationFailure
from .multi_runtime_lock import MultiImageRuntimeLock,verify_installed_runtime
from .multi_admission import PinnedMultiImageAdmission
from .multi_evidence import multi_evidence_resolver


@dataclass(frozen=True)
class MultiImageRuntimeOptions:
    operation:str
    runtime_lock:MultiImageRuntimeLock
    runtime_sha:str
    pin:dict
    policy:dict
    max_pixels:int


@dataclass(frozen=True)
class MultiImageInstallation:
    objects:object
    bucket:str
    options:MultiImageRuntimeOptions


def multi_operations(config,database,installation,*,verify_color):
    if (not isinstance(installation,MultiImageInstallation) or
        not isinstance(installation.options,MultiImageRuntimeOptions) or
        not isinstance(installation.bucket,str) or not 1<=len(installation.bucket)<=255 or
        not callable(getattr(installation.objects,'get_object',None)) or
        not callable(getattr(database,'multi_qualification_evidence',None)) or
        not callable(getattr(database,'multi_capabilities_ready',None)) or not callable(verify_color)):
        raise ValueError('MULTI_INSTALLATION_INVALID')
    options=deepcopy(installation.options)
    lock=MultiImageRuntimeLock.model_validate_json(canonical_bytes(options.runtime_lock))
    verify_installed_runtime(lock,options.runtime_sha)
    resolver=multi_evidence_resolver(database,installation.objects,installation.bucket)
    # Validate every pin/limit/policy before registering the operation.
    PinnedMultiImageAdmission(operation=options.operation,runtime_lock=lock,runtime_sha=options.runtime_sha,pin=options.pin,
        policy=options.policy,max_pixels=options.max_pixels,resolve_evidence=resolver,verify_color=verify_color)
    if (not isinstance(options.policy,dict) or options.policy.get('operation')!=options.operation
        or options.policy.get('operation_version')!='1' or options.policy.get('backend')!='cpu'
        or options.policy.get('license_cleared') is not True or canonical_hash(options.policy)!=options.pin.get('policy_sha')):
        raise ValueError('MULTI_INSTALLATION_POLICY_INVALID')
    if database.multi_capabilities_ready() is not True:
        raise ValueError('MULTI_SQL_CAPABILITIES_UNAVAILABLE')
    from photo_worker.runtime.artifacts import TaskArtifacts
    from .multi_adapter import execute
    roles=frozenset({'document','working_image','pixels','alpha','mask_pixels','mask','report','registration','warp_field'})
    def run(ctx):
        command=ctx.lease.command;profile=ctx.lease.execution_profile
        if (command.operation!=options.operation or command.operation_version!='1' or command.resource_class!='cpu' or not isinstance(profile,dict) or
            profile.get('multi_runtime_sha')!=options.runtime_sha):raise OperationFailure('CONFIG_INVALID')
        ctx.check_active();verify_installed_runtime(lock,options.runtime_sha)
        admission=PinnedMultiImageAdmission(operation=options.operation,runtime_lock=lock,runtime_sha=options.runtime_sha,pin=deepcopy(options.pin),
            policy=deepcopy(options.policy),max_pixels=options.max_pixels,resolve_evidence=resolver,verify_color=verify_color)
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            result=execute(ctx,artifacts,admission,operation=options.operation)
            ctx.check_active();verify_installed_runtime(lock,options.runtime_sha)
            ctx.check_active()
            return result
    return [Operation(options.operation,'cpu',run)]

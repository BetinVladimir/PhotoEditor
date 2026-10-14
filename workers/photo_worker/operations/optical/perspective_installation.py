"""Explicit operator installation for qualified perspective CPU execution."""
from dataclasses import dataclass
from copy import deepcopy
from photo_worker.contracts.base import canonical_bytes,canonical_hash
from photo_worker.runtime.executor import Operation,OperationFailure
from .perspective_runtime_lock import PerspectiveRuntimeLock,verify_installed_runtime
from .perspective_admission import PinnedPerspectiveAdmission
from .perspective_evidence import perspective_evidence_resolver


@dataclass(frozen=True)
class PerspectiveRuntimeOptions:
    runtime_lock:PerspectiveRuntimeLock
    runtime_sha:str
    pin:dict
    policy:dict
    max_pixels:int


@dataclass(frozen=True)
class PerspectiveInstallation:
    objects:object
    bucket:str
    options:PerspectiveRuntimeOptions


def perspective_operations(config,database,installation,*,verify_color):
    if (not isinstance(installation,PerspectiveInstallation) or
        not isinstance(installation.options,PerspectiveRuntimeOptions) or
        not isinstance(installation.bucket,str) or not 1<=len(installation.bucket)<=255 or
        not callable(getattr(installation.objects,'get_object',None)) or
        not callable(getattr(database,'perspective_qualification_evidence',None)) or
        not callable(getattr(database,'perspective_capabilities_ready',None)) or not callable(verify_color)):
        raise ValueError('PERSPECTIVE_INSTALLATION_INVALID')
    options=deepcopy(installation.options)
    lock=PerspectiveRuntimeLock.model_validate_json(canonical_bytes(options.runtime_lock))
    verify_installed_runtime(lock,options.runtime_sha)
    resolver=perspective_evidence_resolver(database,installation.objects,installation.bucket)
    # Validate every pin/limit/policy before registering the operation.
    PinnedPerspectiveAdmission(runtime_lock=lock,runtime_sha=options.runtime_sha,pin=options.pin,
        policy=options.policy,max_pixels=options.max_pixels,resolve_evidence=resolver,verify_color=verify_color)
    if (not isinstance(options.policy,dict) or options.policy.get('operation')!='optical.perspective'
        or options.policy.get('operation_version')!='1' or options.policy.get('backend')!='cpu'
        or options.policy.get('license_cleared') is not True or canonical_hash(options.policy)!=options.pin.get('policy_sha')):
        raise ValueError('PERSPECTIVE_INSTALLATION_POLICY_INVALID')
    if database.perspective_capabilities_ready() is not True:
        raise ValueError('PERSPECTIVE_SQL_CAPABILITIES_UNAVAILABLE')
    from photo_worker.runtime.artifacts import TaskArtifacts
    from .perspective_adapter import execute
    roles=frozenset({'document','working_image','pixels','alpha','mask_pixels','mask','report','transform'})
    def run(ctx):
        command=ctx.lease.command;profile=ctx.lease.execution_profile
        if (command.operation!='optical.perspective' or command.operation_version!='1' or command.resource_class!='cpu' or not isinstance(profile,dict) or
            profile.get('perspective_runtime_sha')!=options.runtime_sha):raise OperationFailure('CONFIG_INVALID')
        ctx.check_active();verify_installed_runtime(lock,options.runtime_sha)
        admission=PinnedPerspectiveAdmission(runtime_lock=lock,runtime_sha=options.runtime_sha,pin=deepcopy(options.pin),
            policy=deepcopy(options.policy),max_pixels=options.max_pixels,resolve_evidence=resolver,verify_color=verify_color)
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            result=execute(ctx,artifacts,admission)
            ctx.check_active();verify_installed_runtime(lock,options.runtime_sha)
            ctx.check_active()
            return result
    return [Operation('optical.perspective','cpu',run)]

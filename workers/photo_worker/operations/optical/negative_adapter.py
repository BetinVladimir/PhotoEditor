"""Install qualified SQL-backed negative CPU execution."""
from copy import deepcopy
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import Operation,OperationFailure
from .negative_runtime_lock import NegativeRuntimeLock,verify_installed_runtime


@dataclass(frozen=True)
class NegativeRuntimeOptions:
    runtime_lock: NegativeRuntimeLock
    runtime_sha: str
    source_max_bytes: int
    ram_max_bytes: int


@dataclass(frozen=True)
class NegativeInstallation:
    objects: object
    bucket: str
    options: NegativeRuntimeOptions


def negative_operations(config,database,objects,*,bucket,options,verify_color):
    if (not isinstance(options,NegativeRuntimeOptions) or not callable(verify_color)
            or not isinstance(bucket,str) or not 1<=len(bucket)<=255
            or not callable(getattr(objects,'get_object',None))
            or any(not callable(getattr(database,name,None)) for name in
                ('negative_input_evidence','negative_lease_ready','negative_source_ready','negative_qualification_evidence'))):
        raise ValueError('NEGATIVE_INSTALLATION_INVALID')
    owned=deepcopy(options)
    if any(type(n) is not int or not 1<=n<=2**53-1 for n in (owned.source_max_bytes,owned.ram_max_bytes)):
        raise ValueError('NEGATIVE_INSTALLATION_INVALID')
    total=getattr(config,'max_scratch_bytes',None)
    if type(total) is not int or not 1<=total<=2**53-1 or owned.source_max_bytes>=total:
        raise ValueError('NEGATIVE_INSTALLATION_SCRATCH_INVALID')
    lock=NegativeRuntimeLock.model_validate_json(canonical_bytes(owned.runtime_lock))
    verify_installed_runtime(lock,owned.runtime_sha)
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.v2.negative_execute import execute_declared_sql_negative
    roles=frozenset({'pixels','working_image','mask_pixels','mask','report','document'})
    def execute(ctx):
        command=ctx.lease.command;profile=ctx.lease.execution_profile
        if (command.operation!='optical.negative' or command.operation_version!='1'
                or command.resource_class!='cpu' or not isinstance(profile,dict) or profile.get('backend')!='cpu'
                or profile.get('renderer_version')!='v2.2'
                or profile.get('negative_runtime_sha')!=owned.runtime_sha):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active();verify_installed_runtime(lock,owned.runtime_sha);ctx.check_active()
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            result=execute_declared_sql_negative(ctx,artifacts,database,qualification_objects=objects,
                bucket=bucket,runtime_lock=lock.model_copy(deep=True),runtime_sha=owned.runtime_sha,
                source_max_bytes=owned.source_max_bytes,ram_max_bytes=owned.ram_max_bytes,verify_color=verify_color)
            ctx.check_active();verify_installed_runtime(lock,owned.runtime_sha);ctx.check_active()
            return result
    return [Operation('optical.negative','cpu',execute)]

"""Install the qualified SQL-backed V28 open-eyes CPU operation."""
from copy import deepcopy
from dataclasses import dataclass
from pathlib import Path
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import Operation,OperationFailure
from .donor_runtime_lock import DonorPortraitRuntimeLock,verify_installed_runtime


@dataclass(frozen=True)
class DonorRuntimeOptions:
    runtime_lock: DonorPortraitRuntimeLock
    runtime_sha: str
    mask_scratch_root: Path
    material_scratch_root: Path
    mask_max_bytes: int
    material_max_bytes: int
    alpha_mode: str = 'require_float32'


@dataclass(frozen=True)
class DonorInstallation:
    objects: object
    bucket: str
    options: DonorRuntimeOptions


def donor_portrait_operations(config,database,objects,*,bucket,options):
    """Startup refuses missing SQL gates, object store or unqualified runtime.

    Scratch roots and runtime locks are operator configuration. Per-attempt
    admission still verifies exact leased budgets and physical input evidence.
    """
    if (not isinstance(options,DonorRuntimeOptions) or not isinstance(bucket,str) or not bucket
            or not callable(getattr(objects,'get_object',None))
            or any(not callable(getattr(database,name,None)) for name in
                ('donor_input_evidence','donor_portrait_lease_ready','donor_source_ready',
                 'donor_qualification_evidence','donor_manual_origin'))):
        raise ValueError('DONOR_INSTALLATION_INVALID')
    owned=deepcopy(options)
    lock=DonorPortraitRuntimeLock.model_validate_json(canonical_bytes(owned.runtime_lock))
    if (any(type(value) is not int or not 1<=value<=2**53-1 for value in
            (owned.mask_max_bytes,owned.material_max_bytes))
            or owned.alpha_mode not in ('require_float32','normalize_uint16')):
        raise ValueError('DONOR_INSTALLATION_INVALID')
    total=getattr(config,'max_scratch_bytes',None)
    if (type(total) is not int or not 1<=total<=2**53-1
            or owned.mask_max_bytes+owned.material_max_bytes>total):
        raise ValueError('DONOR_INSTALLATION_SCRATCH_INVALID')
    for root in (owned.mask_scratch_root,owned.material_scratch_root):
        if not isinstance(root,Path) or not root.is_dir() or root.is_symlink():
            raise ValueError('DONOR_INSTALLATION_INVALID')
    verify_installed_runtime(lock,owned.runtime_sha)
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.v2.portrait_open_eyes_execute import execute_declared_sql_open_eyes
    roles=frozenset({'pixels','working_image','mask_pixels','mask','report','document'})
    def execute(ctx):
        command=ctx.lease.command;profile=ctx.lease.execution_profile
        if (command.operation!='portrait.open_eyes' or command.operation_version!='1'
                or command.resource_class!='cpu' or profile.get('backend')!='cpu'
                or profile.get('renderer_version')!='v2.2'
                or profile.get('portrait_runtime_sha')!=owned.runtime_sha):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active();verify_installed_runtime(lock,owned.runtime_sha);ctx.check_active()
        with TaskArtifacts.create(ctx,config,roles) as artifacts:
            return execute_declared_sql_open_eyes(ctx,artifacts,database,
                qualification_objects=objects,bucket=bucket,runtime_lock=lock.model_copy(deep=True),
                expected_runtime_sha=owned.runtime_sha,source_origin=None,patch_size=None,
                mask_scratch_root=owned.mask_scratch_root,mask_max_bytes=owned.mask_max_bytes,
                material_scratch_root=owned.material_scratch_root,
                material_max_bytes=owned.material_max_bytes,alpha_mode=owned.alpha_mode)
    return [Operation('portrait.open_eyes','cpu',execute)]

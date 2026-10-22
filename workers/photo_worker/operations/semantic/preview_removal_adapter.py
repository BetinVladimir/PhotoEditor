"""Explicit removal adapter; SQL/runtime installer is mandatory before registration."""
from copy import deepcopy
from types import SimpleNamespace
from .preview_removal_inputs import removal_declared_inputs
from hashlib import sha256
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticRemovalPreviewInput
from photo_worker.contracts.images import WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from .preview_adapter import ExposurePreviewInstallation, preview_inputs_declared
from .preview_binding import bind_removal_preview_source


class RemovalPreviewInstallation(ExposurePreviewInstallation):
    """Distinct installer type prevents accidental exposure-only admission."""
    pass


def execute_removal_preview_task(ctx,artifacts,installation,pool,backend):
    if not isinstance(installation,RemovalPreviewInstallation):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    lease=deepcopy(ctx.lease);profile=deepcopy(lease.execution_profile)
    command=lease.command
    if (command.operation!='semantic.preview_remove_object' or command.operation_version!='1'
            or command.resource_class!='gpu' or profile.get('backend')!=backend
            or backend not in ('cpu','cuda','mps')
            or profile.get('renderer_version')!='v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict) or any(type(estimate.get(key)) is not int
            or not 0<=estimate[key]<=2**53-1 for key in
            ('ram_bytes','scratch_bytes','vram_bytes','output_bytes')):
        raise OperationFailure('CONFIG_INVALID')
    # Match backend/SQL admission before reading any artifact or loading a model.
    # Metal is bounded by the actual resource owner, without the CUDA 10GB cap.
    if ((backend=='cpu' and estimate['vram_bytes']!=0)
            or (backend!='cpu' and estimate['vram_bytes']<1)
            or (backend=='cuda' and estimate['vram_bytes']>10_000_000_000)):
        raise OperationFailure('CONFIG_INVALID')
    def active():
        ctx.check_active()
        if ctx.lease!=lease:
            raise OperationFailure('TASK_INPUTS_INVALID')
    active()
    raw=artifacts.contract_bytes(command.params,SemanticRemovalPreviewInput)
    active()
    params=SemanticRemovalPreviewInput.model_validate_json(raw)
    inputs=list(removal_declared_inputs(artifacts,check_active=active))
    if (canonical_bytes(params)!=raw or sha256(raw).hexdigest()!=command.params.sha256
            or params.tenant_id!=command.tenant_id or not preview_inputs_declared(SimpleNamespace(inputs=inputs),params)):
        raise OperationFailure('TASK_INPUTS_INVALID')
    from photo_worker.v2.removal_qualification_pin import RemovalQualificationPin
    from photo_worker.v2.adoption import AdoptionInvalid
    try:
        pin=RemovalQualificationPin.from_value(profile.get('qualification'))
    except AdoptionInvalid:
        raise OperationFailure('TASK_INPUTS_INVALID') from None
    if pin.backend!=backend or (params.model_lock_sha,params.runtime_lock_sha,params.device_profile_sha)!=(
            pin.model_lock_sha,pin.runtime_lock_sha,pin.device_profile_sha):
        raise OperationFailure('TASK_INPUTS_INVALID')
    def admission(value):
        active()
        if canonical_bytes(value)!=raw:
            return False
        if installation.verify_runtime(value.model_copy(deep=True),deepcopy(profile)) is not True:
            return False
        active()
        accepted=installation.verify_admission(deepcopy(lease),raw) is True
        active()
        return accepted
    if not admission(params):
        raise OperationFailure('TASK_INPUTS_INVALID')
    source_raw=artifacts.contract_bytes(params.source.source,WorkingImage)
    active()
    source=bind_removal_preview_source(params,source_raw)
    if estimate['scratch_bytes']<12*source.canvas.width*source.canvas.height+16384:
        raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
    def origin(binding,root,binary):
        active()
        if binding!=params.source or root!=params.protection:
            return False
        accepted=installation.verify_origin(deepcopy(lease),raw,
            binding.model_copy(deep=True),root.model_copy(deep=True),binary.model_copy(deep=True)) is True
        active()
        return accepted
    ctx.progress(0,1)
    if installation.presentation is None:
        raise OperationFailure('CONFIG_INVALID')
    if installation.presentation is not None:
        from .preview_removal_complete import execute_complete_removal_preview
        pins = installation.presentation
        def runtime():
            active()
            accepted = installation.verify_runtime(params.model_copy(deep=True),deepcopy(profile)) is True
            active()
            return accepted
        pixels = source.canvas.width*source.canvas.height
        if estimate['scratch_bytes'] < 32*pixels+4*16384+16777216+5*min(pixels,2560*2560)+1048576+65536:
            raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
        if pool.lock.lock_id != params.model_lock_sha:
            raise OperationFailure('MODEL_LOCK_MISMATCH')
        with pool.acquire(params.model_id,backend) as engine:
            complete = execute_complete_removal_preview(artifacts,params,engine,
            icc_root=pins.icc_root,icc_lock_sha=pins.icc_lock_sha,srgb_sha=pins.srgb_sha,
            verify_admission=admission,verify_origin=origin,verify_color=installation.verify_color,
            verify_runtime=runtime,check_active=active,max_bytes=estimate['ram_bytes'],
            max_output_bytes=estimate['output_bytes'])
        active();ctx.progress(1,1);active()
        return OperationResult(outputs=[complete.presentation],review_required=False,
            bytes_written=complete.bytes_written,peak_device_bytes=0 if backend=='cpu' else None)


def removal_preview_operations(config,*,installation_factory,pool,backend):
    if (not callable(installation_factory) or backend not in ('cpu','cuda','mps')
            or not callable(getattr(pool,'acquire',None))):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    def run(ctx):
        from photo_worker.v2.adoption import AdoptionInvalid
        try:
            with TaskArtifacts.create(ctx,config,frozenset({'pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation'})) as artifacts:
                return execute_removal_preview_task(ctx,artifacts,installation_factory(ctx,artifacts),pool,backend)
        except ModelError as error:
            raise OperationFailure(str(error)) from error
        except (AdoptionInvalid,ValueError):
            raise OperationFailure('TASK_INPUTS_INVALID') from None
    return [Operation('semantic.preview_remove_object','gpu',run,'1')]

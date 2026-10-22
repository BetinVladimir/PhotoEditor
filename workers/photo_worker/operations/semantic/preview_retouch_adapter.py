"""Explicit CPU retouch operation; deployment startup/catalog registration is separate."""
from copy import deepcopy
from .task_route import require_cpu_semantic_route
from photo_worker.contracts.semantic_preview import SemanticRetouchPreviewInput
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from photo_worker.v2.adoption import AdoptionInvalid
from .preview_adapter import PreviewPresentationPins
from .preview_retouch_installation import RetouchPreviewInstallation
from .preview_retouch_complete import execute_complete_retouch_preview
from .preview_retouch_output_closure import ROLES
from .preview_retouch_inputs import retouch_declared_inputs


def execute_retouch_preview_task(ctx, artifacts, installation, presentation):
    if not isinstance(installation, RetouchPreviewInstallation) or not isinstance(presentation, PreviewPresentationPins):
        raise OperationFailure('CONFIG_INVALID')
    require_cpu_semantic_route(ctx, 'semantic.preview_retouch_region')
    installation.check_active()
    lease = deepcopy(ctx.lease)
    command = lease.command
    profile = deepcopy(lease.execution_profile)
    if (command.operation != 'semantic.preview_retouch_region' or command.operation_version != '1'
            or command.resource_class != 'cpu' or profile.get('backend') != 'cpu'):
        raise OperationFailure('CONFIG_INVALID')
    estimate = profile.get('resource_estimate')
    if (not isinstance(estimate, dict) or any(type(estimate.get(key)) is not int
            or not 1 <= estimate[key] <= 2**53-1 for key in ('ram_bytes','scratch_bytes','output_bytes'))
            or type(estimate.get('vram_bytes')) is not int or estimate['vram_bytes'] != 0):
        raise OperationFailure('CONFIG_INVALID')
    params = SemanticRetouchPreviewInput.model_validate_json(installation.params_raw)
    def active():
        installation.check_active()
        ctx.check_active()
        if ctx.lease != lease:
            raise AdoptionInvalid('SEMANTIC_RETOUCH_LEASE_CHANGED')
    active()
    ctx.progress(0,1)
    result = execute_complete_retouch_preview(artifacts, params, installation,
        declared_inputs=list(retouch_declared_inputs(artifacts,check_active=active)), icc_root=presentation.icc_root,
        icc_lock_sha=presentation.icc_lock_sha, srgb_sha=presentation.srgb_sha,
        max_ram_bytes=estimate['ram_bytes'], max_output_bytes=estimate['output_bytes'],
        verify_admission=installation.verify_admission, verify_origin=installation.verify_protection_origin,
        verify_color=installation.verify_color, verify_runtime=installation.verify_runtime, check_active=active)
    active()
    ctx.progress(1,1)
    active()
    return OperationResult(outputs=[result.presentation], review_required=False,
        bytes_written=result.bytes_written, peak_device_bytes=0)


def retouch_preview_operations(config, *, installation_factory, presentation):
    if not callable(installation_factory) or not isinstance(presentation, PreviewPresentationPins):
        raise ValueError('SEMANTIC_RETOUCH_INSTALLATION_REQUIRED')
    def run(ctx):
        require_cpu_semantic_route(ctx, 'semantic.preview_retouch_region')
        try:
            with TaskArtifacts.create(ctx, config, ROLES) as artifacts:
                installation = installation_factory(ctx, artifacts)
                return execute_retouch_preview_task(ctx, artifacts, installation, presentation)
        except (AdoptionInvalid, ValueError):
            raise OperationFailure('TASK_INPUTS_INVALID') from None
    return [Operation('semantic.preview_retouch_region', 'cpu', run, '1')]

"""Explicit worker installation for approved local exposure; no default authority."""
from copy import deepcopy
from dataclasses import dataclass
from photo_worker.contracts.base import ArtifactRef, canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from .exposure_admission import SQLLocalExposureAdmission
from .exposure_protection import sql_local_exposure_protection_origin
from .exposure_execution import execute_and_publish_local_exposure
from .exposure_source import bind_local_exposure_source


@dataclass(frozen=True)
class LocalExposureInstallation:
    database: object
    resolve_protection: object
    verify_color: object
    verify_runtime: object

    def __post_init__(self):
        if not all(callable(value) for value in (self.resolve_protection, self.verify_color,
                self.verify_runtime, getattr(self.database, 'admit_semantic_local_exposure', None),
                getattr(self.database, 'local_exposure_protection_origin', None))):
            raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')


def execute_exposure(ctx, artifacts, installation):
    if not isinstance(installation, LocalExposureInstallation):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    profile = deepcopy(ctx.lease.execution_profile)
    if profile.get('backend') != 'cpu' or profile.get('renderer_version') != 'v2.2':
        raise OperationFailure('CONFIG_INVALID')
    estimate = profile.get('resource_estimate')
    keys = ('ram_bytes', 'scratch_bytes', 'vram_bytes', 'output_bytes')
    if not isinstance(estimate, dict) or any(type(estimate.get(k)) is not int
            or not 0 <= estimate[k] <= 2**53-1 for k in keys):
        raise OperationFailure('CONFIG_INVALID')
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(artifacts.params(ApprovedSemanticEdit)))
    sql_admission = SQLLocalExposureAdmission(installation.database, ctx)
    def admission(value):
        ctx.check_active()
        if installation.verify_runtime(value.model_copy(deep=True), deepcopy(profile)) is not True:
            return False
        ctx.check_active()
        return sql_admission(value)
    if admission(edit) is not True:
        raise OperationFailure('CONFIG_INVALID')
    root = ArtifactRef.model_validate_json(canonical_bytes(
        installation.resolve_protection(edit.model_copy(deep=True))))
    if root.artifact_id.int == 0:
        raise OperationFailure('CONFIG_INVALID')
    origin = sql_local_exposure_protection_origin(installation.database, ctx.lease,
        canonical_bytes(edit), check_active=ctx.check_active)
    source = bind_local_exposure_source(edit, artifacts.contract_bytes(edit.source.source, WorkingImage))
    if estimate['scratch_bytes'] < 12*source.canvas.width*source.canvas.height+16384:
        raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
    # Actual reserve capacity is also enforced by TaskArtifacts/ScratchSpace.
    ctx.progress(0, 1)
    staged = execute_and_publish_local_exposure(artifacts, edit, protection_ref=root,
        verify_origin=origin, verify_color=installation.verify_color,
        verify_admission=admission, check_active=ctx.check_active,
        max_bytes=estimate['ram_bytes'], max_output_bytes=estimate['output_bytes'])
    ctx.check_active()
    ctx.progress(1, 1)
    return OperationResult(outputs=[staged.root], review_required=staged.review_required, bytes_written=staged.bytes_written,
        peak_device_bytes=0)


def local_exposure_operations(config, *, installation_factory):
    if not callable(installation_factory):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    def run(ctx):
        from photo_worker.v2.adoption import AdoptionInvalid
        try:
            with TaskArtifacts.create(ctx, config, frozenset({'pixels', 'working_image', 'report', 'document'})) as artifacts:
                return execute_exposure(ctx, artifacts, installation_factory(ctx, artifacts))
        except AdoptionInvalid:
            # Qualification/scope proof failure is permanent for this attempt.
            # Never persist validator details, object coordinates or SQL text.
            raise OperationFailure('TASK_INPUTS_INVALID') from None
    return [Operation('semantic.local_exposure', 'cpu', run, '1')]


def locked_local_exposure_installation(*, database, resolve_protection, verify_runtime, icc_registry):
    """Construct an explicit installation with verified bundled ICC identity.

    Protection authority and live runtime/SQL qualification must be supplied by
    the service installer. This constructor does not register/enable operations.
    """
    from .exposure_color import local_exposure_icc_verifier
    return LocalExposureInstallation(database=database,
        resolve_protection=resolve_protection, verify_color=local_exposure_icc_verifier(icc_registry),
        verify_runtime=verify_runtime)

"""Explicit preview worker adapter; not registered without a production installer."""
from copy import deepcopy
from hashlib import sha256
from dataclasses import dataclass
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import SemanticExposurePreviewInput
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationFailure,OperationResult
from .preview_binding import bind_exposure_preview_source
from .preview_stage import execute_and_stage_exposure_preview


@dataclass(frozen=True)
class PreviewPresentationPins:
    icc_root: object
    icc_lock_sha: str
    srgb_sha: str

    def __post_init__(self):
        from pathlib import Path
        from photo_worker.v2.semantic_qualification_pin import _sha
        if not isinstance(self.icc_root, Path) or not self.icc_root.is_absolute() or not all(
                _sha(value) for value in (self.icc_lock_sha, self.srgb_sha)):
            raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')


@dataclass(frozen=True)
class ExposurePreviewInstallation:
    verify_admission: object
    verify_origin: object
    verify_color: object
    verify_runtime: object
    presentation: PreviewPresentationPins | None = None

    def __post_init__(self):
        if self.presentation is not None and not isinstance(self.presentation, PreviewPresentationPins):
            raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
        if not all(callable(value) for value in (self.verify_admission,
                self.verify_origin,self.verify_color,self.verify_runtime)):
            raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')



def preview_inputs_declared(command,params):
    """Exact proposal roots must be declared lease inputs, never discovered IO."""
    from photo_worker.contracts.base import ArtifactRef
    inputs=getattr(command,'inputs',None)
    if not isinstance(inputs,list) or len(inputs)>512:
        return False
    seen={}
    for ref in inputs:
        if not isinstance(ref,ArtifactRef) or ref.artifact_id.int==0 or ref.artifact_id in seen:
            return False
        seen[ref.artifact_id]=ref.sha256
    return all(seen.get(ref.artifact_id)==ref.sha256 for ref in
        (params.plan,params.intent,params.candidate,params.source.source,params.protection))


def execute_preview_task(ctx,artifacts,installation):
    if not isinstance(installation,ExposurePreviewInstallation):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    lease=deepcopy(ctx.lease);profile=deepcopy(lease.execution_profile)
    command=lease.command
    if (command.operation!='semantic.preview_local_exposure' or command.operation_version!='1'
            or command.resource_class!='cpu' or profile.get('backend')!='cpu'
            or profile.get('renderer_version')!='v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    estimate=profile.get('resource_estimate')
    if not isinstance(estimate,dict) or any(type(estimate.get(key)) is not int
            or not 0<=estimate[key]<=2**53-1 for key in
            ('ram_bytes','scratch_bytes','vram_bytes','output_bytes')):
        raise OperationFailure('CONFIG_INVALID')
    if estimate['vram_bytes']!=0:
        raise OperationFailure('CONFIG_INVALID')
    raw=artifacts.contract_bytes(command.params,SemanticExposurePreviewInput)
    params=SemanticExposurePreviewInput.model_validate_json(raw)
    if (canonical_bytes(params)!=raw or sha256(raw).hexdigest()!=command.params.sha256
            or params.tenant_id!=command.tenant_id or not preview_inputs_declared(command,params)):
        raise OperationFailure('TASK_INPUTS_INVALID')
    from photo_worker.v2.exposure_qualification_pin import ExposureQualificationPin
    from photo_worker.v2.adoption import AdoptionInvalid
    try:
        pin=ExposureQualificationPin.from_value(profile.get('qualification'))
    except AdoptionInvalid:
        raise OperationFailure('TASK_INPUTS_INVALID') from None
    if (params.model_lock_sha,params.runtime_lock_sha,params.device_profile_sha)!=(
            pin.model_lock_sha,pin.runtime_lock_sha,pin.device_profile_sha):
        raise OperationFailure('TASK_INPUTS_INVALID')
    def active():
        ctx.check_active()
        if ctx.lease!=lease:
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
    source=bind_exposure_preview_source(params,artifacts.contract_bytes(params.source.source,WorkingImage))
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
    if installation.presentation is not None:
        from .preview_complete import execute_complete_exposure_preview
        pins = installation.presentation
        def runtime():
            active()
            accepted = installation.verify_runtime(params.model_copy(deep=True),deepcopy(profile)) is True
            active()
            return accepted
        pixels = source.canvas.width*source.canvas.height
        if estimate['scratch_bytes'] < 32*pixels+4*16384+16777216+5*min(pixels,2560*2560)+1048576+65536:
            raise OperationFailure('SCRATCH_ADMISSION_EXCEEDED')
        complete = execute_complete_exposure_preview(artifacts,params,
            icc_root=pins.icc_root,icc_lock_sha=pins.icc_lock_sha,srgb_sha=pins.srgb_sha,
            verify_admission=admission,verify_origin=origin,verify_color=installation.verify_color,
            verify_runtime=runtime,check_active=active,max_bytes=estimate['ram_bytes'],
            max_output_bytes=estimate['output_bytes'])
        active();ctx.progress(1,1)
        return OperationResult(outputs=[complete.presentation],review_required=False,
            bytes_written=complete.bytes_written,peak_device_bytes=0)
    staged=execute_and_stage_exposure_preview(artifacts,params,
        verify_admission=admission,verify_origin=origin,verify_color=installation.verify_color,
        check_active=active,max_bytes=estimate['ram_bytes'],max_output_bytes=estimate['output_bytes'])
    active();ctx.progress(1,1)
    return OperationResult(outputs=[staged.evidence],review_required=False,
        bytes_written=staged.bytes_written,peak_device_bytes=0)


def exposure_preview_operations(config,*,installation_factory):
    if not callable(installation_factory):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    def run(ctx):
        from photo_worker.v2.adoption import AdoptionInvalid
        try:
            with TaskArtifacts.create(ctx,config,frozenset({'pixels','working_image','preview_evidence','before','after','affected','protected','preview_presentation'})) as artifacts:
                return execute_preview_task(ctx,artifacts,installation_factory(ctx,artifacts))
        except (AdoptionInvalid,ValueError):
            raise OperationFailure('TASK_INPUTS_INVALID') from None
    return [Operation('semantic.preview_local_exposure','cpu',run,'1')]

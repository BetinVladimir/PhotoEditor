"""Explicit byte-copy operation; registration requires SQL receipt installer."""
from copy import deepcopy
from .preview_retouch_inputs import retouch_declared_inputs
from .task_route import require_cpu_semantic_route
from dataclasses import dataclass
from hashlib import sha256
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from photo_worker.runtime.artifacts import TaskArtifacts
from photo_worker.runtime.executor import Operation,OperationFailure,OperationResult
from .preview_retouch_adopt_execute import execute_shown_retouch_adoption


@dataclass(frozen=True)
class ShownRetouchInstallation:
    verify_admission: object
    verify_runtime: object
    verify_color: object

    def __post_init__(self):
        if not all(callable(fn) for fn in (self.verify_admission,self.verify_runtime,self.verify_color)):
            raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')


def execute_shown_retouch_task(ctx,artifacts,installation):
    if not isinstance(installation,ShownRetouchInstallation):raise OperationFailure('CONFIG_INVALID')
    require_cpu_semantic_route(ctx, 'semantic.adopt_shown_retouch_region')
    ctx.check_active();lease=deepcopy(ctx.lease);command=lease.command;profile=deepcopy(lease.execution_profile)
    def active():
        ctx.check_active()
        if ctx.lease!=lease:raise OperationFailure('TASK_INPUTS_INVALID')
    if (command.operation!='semantic.adopt_shown_retouch_region' or command.operation_version!='1'
            or command.resource_class!='cpu' or profile.get('backend')!='cpu'
            or profile.get('renderer_version')!='v2.2'):
        raise OperationFailure('CONFIG_INVALID')
    estimate=profile.get('resource_estimate')
    if (not isinstance(estimate,dict) or set(estimate)!=set(('ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'))
            or any(type(estimate.get(key)) is not int
            or not 0<=estimate[key]<=2**53-1 for key in
            ('ram_bytes','vram_bytes','scratch_bytes','output_bytes','pixel_count'))
            or estimate['vram_bytes']!=0 or not 1<=estimate['pixel_count']<=200000000
            or estimate['ram_bytes']<64*1024**2+1
            or estimate['scratch_bytes']<24*estimate['pixel_count']+2*16384+16*1024**2
            or estimate['output_bytes']<12*estimate['pixel_count']+16384+1048576):
        raise OperationFailure('CONFIG_INVALID')
    active()
    raw=artifacts.contract_bytes(command.params,ShownRetouchAdoptionInput)
    active()
    params=ShownRetouchAdoptionInput.model_validate_json(raw)
    if (canonical_bytes(params)!=raw or sha256(raw).hexdigest()!=command.params.sha256
            or params.tenant_id!=command.tenant_id or params.child_run_id!=command.run_id):
        raise OperationFailure('TASK_INPUTS_INVALID')
    inputs=list(retouch_declared_inputs(artifacts,check_active=active))
    if not isinstance(inputs,list) or len(inputs)>512 or any(not isinstance(ref,ArtifactRef) for ref in inputs):
        raise OperationFailure('TASK_INPUTS_INVALID')
    declared={ref.artifact_id:ref.sha256 for ref in inputs}
    if len(declared)!=len(inputs) or any(ref.artifact_id.int==0 for ref in inputs) or any(
            declared.get(ref.artifact_id)!=ref.sha256 for ref in (params.approved_edit,params.presentation,
                params.numerical_evidence,params.working_image,params.pixels)):
        raise OperationFailure('TASK_INPUTS_INVALID')
    def runtime():
        active()
        accepted=installation.verify_runtime(params.model_copy(deep=True),deepcopy(profile)) is True
        active();return accepted
    def admission(value):
        active()
        if canonical_bytes(value)!=raw:return False
        accepted=installation.verify_admission(deepcopy(lease),raw) is True
        active();return accepted
    ctx.progress(0,1)
    staged=execute_shown_retouch_adoption(artifacts,params,verify_admission=admission,
        verify_runtime=runtime,verify_color=installation.verify_color,check_active=active,
        max_output_bytes=estimate['output_bytes'],expected_pixel_count=estimate['pixel_count'])
    active();ctx.progress(1,1);active()
    return OperationResult(outputs=[staged.image],review_required=False,
        bytes_written=staged.bytes_written,peak_device_bytes=0)


def shown_retouch_operations(config,*,installation_factory):
    if not callable(installation_factory):raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    def run(ctx):
        from photo_worker.v2.adoption import AdoptionInvalid
        require_cpu_semantic_route(ctx, 'semantic.adopt_shown_retouch_region')
        try:
            with TaskArtifacts.create(ctx,config,frozenset({'pixels','working_image'})) as artifacts:
                return execute_shown_retouch_task(ctx,artifacts,installation_factory(ctx,artifacts))
        except (AdoptionInvalid,ValueError):
            raise OperationFailure('TASK_INPUTS_INVALID') from None
    return [Operation('semantic.adopt_shown_retouch_region','cpu',run,'1')]

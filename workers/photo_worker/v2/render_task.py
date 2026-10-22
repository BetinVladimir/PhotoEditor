"""Full recipe task boundary; SQL admission and qualified renderer are mandatory."""
from typing import Protocol
import re
from hashlib import sha256
from photo_worker.runtime.executor import OperationFailure, OperationResult
from .render_recipe import decode_frozen_render_recipe
from .render_inputs import read_render_document, load_frozen_render_inputs
from .render_resources import validate_render_execution_profile
from .render_prepare import admit_render_inputs
from .render_result import StagedRenderResult
from .render_qualification_pin import RenderQualificationPin


class RenderTaskAdmission(Protocol):
    def admit(self, ctx, reference, recipe, raw):
        """Prove task/fence, registered root, complete closure and live authority."""
        ...

    def qualification(self, ctx, reference, recipe, raw):
        """Read the exact admitted lease's immutable full qualification pin."""
        ...


class SQLRenderTaskAdmission:
    def __init__(self, database):
        self.database = database

    def admit(self, ctx, reference, recipe, raw):
        ctx.check_active()
        command = ctx.lease.command
        if (command.operation != 'render.compose' or command.operation_version != '1'
                or command.resource_class != 'gpu' or reference != command.params
                or not isinstance(raw, bytes) or not 2 <= len(raw) <= 1048576
                or sha256(raw).hexdigest() != reference.sha256):
            raise OperationFailure('INPUT_INVALID')
        if decode_frozen_render_recipe(raw) != recipe:
            raise OperationFailure('INPUT_INVALID')
        if not self.database.admit_render_recipe(ctx.lease, raw):
            raise OperationFailure('INPUT_INVALID')
        ctx.check_active()

    def qualification(self, ctx, reference, recipe, raw):
        self.admit(ctx, reference, recipe, raw)
        value=self.database.render_qualification(ctx.lease,raw)
        if value is None:raise OperationFailure('INPUT_INVALID')
        try:return RenderQualificationPin.from_value(value)
        except ValueError as error:raise OperationFailure('INPUT_INVALID') from error

    def inputs(self,ctx,raw,pin):
        from dataclasses import replace
        from .render_adoption_inputs import decode_render_adoption_inputs
        from .adoption import AdoptionInvalid,ArtifactRef
        ctx.check_active()
        row=self.database.render_cache_context(ctx.lease,raw)
        try:
            if not isinstance(row,(tuple,list)) or len(row)!=5:
                raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_STALE')
            scoped=replace(decode_render_adoption_inputs(row[:4]),
                qualification_pin=RenderQualificationPin.from_value(row[4]))
            from .render_adoption_inputs import decode_executed_base_reference
            scoped=replace(scoped, executed_base_receipt=decode_executed_base_reference(
                self.database.render_executed_base_receipt(ctx.lease, raw)))
            reference=ctx.lease.command.params
            if (scoped.recipe!=ArtifactRef(reference.artifact_id,reference.sha256)
                    or scoped.execution_profile()!=ctx.lease.execution_profile
                    or scoped.qualification_pin!=pin):
                raise AdoptionInvalid('RENDER_ADOPTION_INPUTS_STALE')
        except AdoptionInvalid as error:
            raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True) from error
        ctx.check_active()
        return scoped


class QualifiedTaskRenderer(Protocol):
    backend: str
    runtime_sha256: str
    model_lock_sha: str
    icc_lock_sha: str
    provider_peak: dict
    qualification_pin: RenderQualificationPin

    def verify_runtime(self):
        """Verify installed runtime, model/ICC and external qualification."""
        ...

    def execute(self, ctx, artifacts, inputs):
        """Use profiled authored execution and actual measured quality evidence."""
        ...


def execute_render_task(ctx, artifacts, admission, renderer):
    if admission is None or renderer is None:
        raise OperationFailure('RENDER_RUNTIME_UNAVAILABLE')
    ctx.check_active()
    profile = ctx.lease.execution_profile
    runtime_sha = getattr(renderer, 'runtime_sha256', None)
    model_sha = getattr(renderer, 'model_lock_sha', None)
    icc_sha = getattr(renderer, 'icc_lock_sha', None)
    backend = getattr(renderer, 'backend', None)
    if (not isinstance(profile, dict) or backend not in ('cpu', 'cuda', 'mps')
            or any(not isinstance(pin, str) or re.fullmatch('[0-9a-f]{64}', pin) is None
                   for pin in (runtime_sha, model_sha, icc_sha))
            or profile.get('backend') != backend
            or profile.get('render_runtime_sha') != runtime_sha
            or profile.get('model_lock_sha') != model_sha
            or profile.get('icc_lock_sha') != icc_sha):
        raise OperationFailure('CONFIG_INVALID')
    renderer.verify_runtime()
    ctx.check_active()
    try:
        reference = ctx.lease.command.params
        raw = read_render_document(artifacts, reference, ctx.check_active)
        recipe = decode_frozen_render_recipe(raw)
        validate_render_execution_profile(profile, recipe, backend=renderer.backend,
            runtime_sha=renderer.runtime_sha256, tile_size=profile.get('tile_size'),
            alpha_mode=profile.get('alpha_mode'))
        admission.admit(ctx, reference, recipe, raw)
        pin = admission.qualification(ctx, reference, recipe, raw)
        if (not isinstance(pin,RenderQualificationPin) or pin!=getattr(renderer,'qualification_pin',None)
                or (pin.backend,pin.runtime_lock_sha,pin.model_lock_sha)!=(backend,runtime_sha,model_sha)):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active()
        inputs = load_frozen_render_inputs(artifacts, reference, check_active=ctx.check_active)
        if inputs.recipe != recipe:
            raise ValueError('RENDER_TASK_RECIPE_CHANGED')
        admit_render_inputs(inputs, profile, (renderer.provider_peak_for(inputs, profile) if hasattr(renderer, "provider_peak_for") else renderer.provider_peak),
            tile_size=profile['tile_size'], alpha_mode=profile['alpha_mode'])
        ctx.check_active()
        staged = None
        if isinstance(admission, SQLRenderTaskAdmission):
            from .render_cache_worker import WorkerRenderCache
            scratch_bound=min(artifacts.scratch.budget_bytes,profile['resource_estimate']['scratch_bytes'])
            if scratch_bound>0:
                from .adoption import AdoptionInvalid
                from photo_worker.runtime.gateway import GatewayError
                try:
                    staged=WorkerRenderCache(admission.database,max_bytes=scratch_bound).try_publish(
                        ctx,artifacts,pin)
                except AdoptionInvalid as error:
                    raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True) from error
                except GatewayError as error:
                    if error.code in ('ARTIFACT_MISSING','ARTIFACT_HASH_MISMATCH'):
                        raise OperationFailure('DEPENDENCY_UNAVAILABLE',retryable=True) from error
                    raise
            ctx.check_active()
        if staged is None:
            if isinstance(renderer,ProfiledTaskRenderer):
                if not isinstance(admission,SQLRenderTaskAdmission):
                    raise OperationFailure('CONFIG_INVALID')
                scoped=admission.inputs(ctx,raw,pin)
                staged=renderer.execute(ctx,artifacts,inputs,authoritative_inputs=scoped)
            else:
                staged = renderer.execute(ctx, artifacts, inputs)
        ctx.check_active()
        if not isinstance(staged, StagedRenderResult):
            raise ValueError('RENDER_TASK_RESULT_INVALID')
        return OperationResult(outputs=[staged.document], bytes_written=staged.bytes_written,
                               review_required=staged.review_required)
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error


class ProfiledTaskRenderer:
    """Wire installed qualified providers to the actual authored executor.

    Provider loader supplies operators, overrides, rendering and measurement
    callbacks plus preparation options. Authoritative task/profile/runtime pins
    cannot be supplied or replaced by that loader.
    """
    def __init__(self, *, backend, runtime_path, runtime_sha256, model_lock_sha,
                 icc_lock_sha, provider_peak, verify_qualification, load_providers, qualification_pin=None, provider_peak_for=None):
        if not callable(verify_qualification) or not callable(load_providers):
            raise OperationFailure('RENDER_RUNTIME_UNAVAILABLE')
        self.backend = backend
        self.runtime_path = runtime_path
        self.runtime_sha256 = runtime_sha256
        self.model_lock_sha = model_lock_sha
        self.icc_lock_sha = icc_lock_sha
        self._provider_peak = tuple(dict(provider_peak).items())
        if provider_peak_for is not None and not callable(provider_peak_for):raise OperationFailure("CONFIG_INVALID")
        self._provider_peak_for=provider_peak_for
        self.qualification_pin = qualification_pin
        self.verify_qualification = verify_qualification
        self.load_providers = load_providers

    def provider_peak_for(self, inputs, profile):
        return self.provider_peak if self._provider_peak_for is None else self._provider_peak_for(inputs, profile)

    @property
    def provider_peak(self):
        # Provider loaders receive task-local state and cannot rewrite admitted
        # installer costs through a shared public mapping.
        return dict(self._provider_peak)

    def verify_runtime(self):
        from .render_runtime import load_render_runtime_lock
        load_render_runtime_lock(self.runtime_path, self.runtime_sha256)
        pin=self.qualification_pin
        if (not isinstance(pin,RenderQualificationPin)
                or (pin.backend,pin.runtime_lock_sha,pin.model_lock_sha)!=(self.backend,self.runtime_sha256,self.model_lock_sha)):
            raise OperationFailure('CONFIG_INVALID')
        self.verify_qualification(pin)

    def execute(self, ctx, artifacts, inputs, *, authoritative_inputs=None):
        from .render_execute import execute_profiled_authored_render
        from .render_adoption_inputs import RenderAdoptionInputs,resolve_render_source_parent
        ctx.check_active()
        if (not isinstance(authoritative_inputs,RenderAdoptionInputs)
                or not isinstance(authoritative_inputs.qualification_pin,RenderQualificationPin)
                or authoritative_inputs.qualification_pin!=self.qualification_pin
                or authoritative_inputs.execution_profile()!=ctx.lease.execution_profile
                or authoritative_inputs.recipe.artifact_id!=ctx.lease.command.params.artifact_id
                or authoritative_inputs.recipe.sha256!=ctx.lease.command.params.sha256):
            raise OperationFailure('CONFIG_INVALID')
        from copy import deepcopy
        inputs=deepcopy(inputs)
        profile=authoritative_inputs.execution_profile()
        source_parent=resolve_render_source_parent(artifacts,authoritative_inputs,
            inputs.recipe.recipe.source.source,check_active=ctx.check_active)
        supplied = self.load_providers(ctx, artifacts, deepcopy(inputs))
        if ctx.lease.execution_profile!=profile:
            raise OperationFailure('CONFIG_INVALID')
        required = {'operators', 'scalar_overrides', 'render_base', 'render_proposal', 'measure', 'preparation'}
        if not isinstance(supplied, dict) or set(supplied) != required or not isinstance(supplied['preparation'], dict):
            raise OperationFailure('CONFIG_INVALID')
        from .render_providers import RenderLayerProviders
        if isinstance(supplied['render_proposal'],RenderLayerProviders) and supplied['operators']!=supplied['render_proposal'].operators:
            raise OperationFailure('CONFIG_INVALID')
        options = dict(supplied['preparation'])
        reserved = {'backend', 'execution_profile', 'runtime_lock_path', 'runtime_lock_sha',
                    'check_active', 'provider_peak', 'tile_size', 'alpha_mode', 'verify_base_runtime','source_result'}
        if reserved.intersection(options):
            raise OperationFailure('CONFIG_INVALID')
        ctx.check_active()
        return execute_profiled_authored_render(artifacts, inputs, supplied['operators'],
            supplied['scalar_overrides'], supplied['render_base'], supplied['render_proposal'],
            self.verify_runtime, supplied['measure'], backend=self.backend,
            runtime_lock_path=self.runtime_path, runtime_lock_sha=self.runtime_sha256,
            execution_profile=profile, provider_peak=self.provider_peak_for(inputs, profile),
            tile_size=profile['tile_size'], alpha_mode=profile['alpha_mode'],
            source_result=source_parent,check_active=ctx.check_active, **options)


def render_task_operations(config, admission=None, renderer=None):
    """Install only with configured admission and qualified full renderer.

    Service configuration and SQL graph/operation registration remain required.
    """
    from photo_worker.runtime.artifacts import TaskArtifacts
    from photo_worker.runtime.executor import Operation
    if admission is None and renderer is None:
        return []
    if admission is None or renderer is None:
        raise OperationFailure('RENDER_RUNTIME_UNAVAILABLE')
    if renderer.backend not in ('cpu', 'cuda', 'mps'):
        raise OperationFailure('CONFIG_INVALID')
    renderer.verify_runtime()
    resource_class = 'gpu'
    roles = frozenset({'document', 'working_image', 'pixels', 'alpha', 'mask_pixels', 'mask', 'report'})
    def run(ctx):
        ctx.check_active()
        command = ctx.lease.command
        if (command.operation != 'render.compose' or command.operation_version != '1'
                or command.resource_class != resource_class):
            raise OperationFailure('CONFIG_INVALID')
        with TaskArtifacts.create(ctx, config, roles) as artifacts:
            return execute_render_task(ctx, artifacts, admission, renderer)
    return [Operation('render.compose', resource_class, run)]

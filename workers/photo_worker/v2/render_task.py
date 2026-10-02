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


class RenderTaskAdmission(Protocol):
    def admit(self, ctx, reference, recipe, raw):
        """Prove task/fence, registered root, complete closure and live authority."""
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


class QualifiedTaskRenderer(Protocol):
    backend: str
    runtime_sha256: str
    model_lock_sha: str
    icc_lock_sha: str
    provider_peak: dict

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
        ctx.check_active()
        inputs = load_frozen_render_inputs(artifacts, reference, check_active=ctx.check_active)
        if inputs.recipe != recipe:
            raise ValueError('RENDER_TASK_RECIPE_CHANGED')
        admit_render_inputs(inputs, profile, renderer.provider_peak,
            tile_size=profile['tile_size'], alpha_mode=profile['alpha_mode'])
        ctx.check_active()
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
                 icc_lock_sha, provider_peak, verify_qualification, load_providers):
        if not callable(verify_qualification) or not callable(load_providers):
            raise OperationFailure('RENDER_RUNTIME_UNAVAILABLE')
        self.backend = backend
        self.runtime_path = runtime_path
        self.runtime_sha256 = runtime_sha256
        self.model_lock_sha = model_lock_sha
        self.icc_lock_sha = icc_lock_sha
        self.provider_peak = dict(provider_peak)
        self.verify_qualification = verify_qualification
        self.load_providers = load_providers

    def verify_runtime(self):
        from .render_runtime import load_render_runtime_lock
        load_render_runtime_lock(self.runtime_path, self.runtime_sha256)
        self.verify_qualification()

    def execute(self, ctx, artifacts, inputs):
        from .render_execute import execute_profiled_authored_render
        ctx.check_active()
        supplied = self.load_providers(ctx, artifacts, inputs)
        required = {'operators', 'scalar_overrides', 'render_base', 'render_proposal', 'measure', 'preparation'}
        if not isinstance(supplied, dict) or set(supplied) != required or not isinstance(supplied['preparation'], dict):
            raise OperationFailure('CONFIG_INVALID')
        options = dict(supplied['preparation'])
        reserved = {'backend', 'execution_profile', 'runtime_lock_path', 'runtime_lock_sha',
                    'check_active', 'provider_peak', 'tile_size', 'alpha_mode', 'verify_base_runtime'}
        if reserved.intersection(options):
            raise OperationFailure('CONFIG_INVALID')
        profile = ctx.lease.execution_profile
        ctx.check_active()
        return execute_profiled_authored_render(artifacts, inputs, supplied['operators'],
            supplied['scalar_overrides'], supplied['render_base'], supplied['render_proposal'],
            self.verify_runtime, supplied['measure'], backend=self.backend,
            runtime_lock_path=self.runtime_path, runtime_lock_sha=self.runtime_sha256,
            execution_profile=profile, provider_peak=self.provider_peak,
            tile_size=profile['tile_size'], alpha_mode=profile['alpha_mode'],
            check_active=ctx.check_active, **options)


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

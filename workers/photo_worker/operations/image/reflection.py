"""V28 reflection worker handler, dormant until a trusted model installation exists."""

from contextlib import ExitStack
from typing import Protocol

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_restoration import ImageReflectionParams
from photo_worker.operations.session.contracts import Calibration
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.executor import OperationFailure, OperationResult

from .reflection_inputs import verify_reflection_inputs
from .reflection_prepare import prepare_reflection
from .reflection_result import stage_reflection_result
from .reflection_tiles import _MAX_PADDED_PIXELS


class ReflectionAdmission(Protocol):
    lock_sha: str

    def admit(self, ctx, params, source, target, protected, calibration) -> bool:
        """Recheck live tenant/version/ICC and exact runtime/device qualification."""

    def acquire(self, ctx, backend: str):
        """Context manager yielding the installed, qualified reflection_rgb engine."""


def require_reflection_admission(decision: object) -> None:
    """Only an explicit live authority grant permits model and artifact I/O."""
    if decision is not True:
        raise OperationFailure('REFLECTION_MODEL_UNAVAILABLE')


def _required_reflection_ram(height: int, width: int, size: object) -> int:
    """Bound the actual reflect-padded accumulator before input or model I/O."""
    if type(size) is not int or not 128 < size <= 2048:
        raise ValueError('MODEL_CONTRACT_ERROR')
    padded = max(height, size) * max(width, size)
    if padded > _MAX_PADDED_PIXELS:
        raise OperationFailure('RESOURCE_OOM')
    return 2 * 1024**3 + 64 * padded


def execute(ctx, artifacts, admission: ReflectionAdmission | None) -> OperationResult:
    """Process one pinned reflection task; never infer authority from Params."""
    if admission is None:
        raise OperationFailure('REFLECTION_MODEL_UNAVAILABLE')
    command = ctx.lease.command
    profile = ctx.lease.execution_profile
    backend = profile.get('backend')
    estimate = profile.get('resource_estimate')
    ram = estimate.get('ram_bytes') if isinstance(estimate, dict) else None
    if (command.operation != 'image.reflection' or command.operation_version != '1'
            or command.resource_class != 'gpu' or backend not in ('cpu', 'cuda')
            or profile.get('renderer_version') != 'v2.2'
            or type(ram) is not int or ram < 2 * 1024**3):
        raise OperationFailure('CONFIG_INVALID')
    ctx.check_active()
    try:
        params = artifacts.params(ImageReflectionParams)
        if len(canonical_bytes(params)) > 65536 or canonical_hash(params) != command.params.sha256:
            raise ValueError('REFLECTION_PARAMS_BINDING_MISMATCH')
        params = ImageReflectionParams.model_validate_json(canonical_bytes(params))
        source = WorkingImage.model_validate_json(canonical_bytes(
            artifacts.contract(params.base.source, WorkingImage)))
        target = MaskRef.model_validate_json(canonical_bytes(
            artifacts.contract(params.mask, MaskRef)))
        protected = MaskRef.model_validate_json(canonical_bytes(
            artifacts.contract(params.protected, MaskRef)))
        calibration = Calibration.model_validate_json(canonical_bytes(
            artifacts.contract(params.calibration, Calibration)))
        verify_reflection_inputs(params, source, target, protected, calibration,
                                 artifacts.declared_input_refs(), admission.lock_sha)
        height, width = source.canvas.height, source.canvas.width
        if ram < 2 * 1024**3 + height * width * 256:
            raise OperationFailure('RESOURCE_OOM')

        def authorize():
            ctx.check_active()
            decision = admission.admit(
                ctx, params.model_copy(deep=True), source.model_copy(deep=True),
                target.model_copy(deep=True), protected.model_copy(deep=True),
                calibration.model_copy(deep=True))
            require_reflection_admission(decision)
            ctx.check_active()

        authorize()
        ctx.progress(0, 3)
        with admission.acquire(ctx, backend) as engine, ExitStack() as stack:
            if getattr(engine, 'size', None) is None:
                raise OperationFailure('MODEL_CONTRACT_ERROR')
            if ram < _required_reflection_ram(height, width, engine.size):
                raise OperationFailure('RESOURCE_OOM')

            def mapped(ref, shape):
                path = stack.enter_context(artifacts.input(ref, suffix='.npy')).path
                return stack.enter_context(read_array(path, shape, 'float32',
                                                      ctx.check_active, unit_interval=True))

            source_pixels = mapped(source.pixels, (height, width, 3))
            target_pixels = mapped(target.artifact, (height, width))
            protected_pixels = mapped(protected.artifact, (height, width))
            ctx.progress(1, 3)
            with prepare_reflection(source_pixels, target_pixels, protected_pixels,
                                    params.strength, source.color.profile_id, engine,
                                    artifacts.scratch, check_active=ctx.check_active) as files:
                authorize()
                result_ref, written = stage_reflection_result(artifacts, params, source, files)
        ctx.progress(3, 3)
        return OperationResult(outputs=[result_ref], review_required=True,
                               bytes_written=written)
    except ValueError as error:
        code = str(error)
        allowed = {'REFLECTION_MODEL_UNAVAILABLE', 'REFLECTION_OUTSIDE_MASK_CHANGED',
                   'MODEL_CONTRACT_ERROR', 'MODEL_OUTPUT_INVALID',
                   'MODEL_INFERENCE_ERROR', 'BACKEND_UNAVAILABLE'}
        raise OperationFailure(code if code in allowed else 'INPUT_INVALID') from error

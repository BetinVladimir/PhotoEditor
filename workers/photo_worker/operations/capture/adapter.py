"""V24 capture assessment on a frozen, source-bound working proxy."""

from contextlib import ExitStack
import numpy as np

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.images import WorkingImage
from photo_worker.models.install import ModelError
from photo_worker.operations.session.contracts import Calibration, ConsentSnapshot, FaceObservations
from photo_worker.runtime.arrays import read_array, read_alpha_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import Operation, OperationFailure, OperationResult
from .assess import assess_capture
from .contracts import CaptureAssessParams, CaptureContext, ShotList
from .evidence import infer_face_evidence


def validate_proxy(source: WorkingImage, proxy: WorkingImage, binding) -> None:
    scale = min(1.0, 2048 / max(source.canvas.width, source.canvas.height))
    expected_width = max(1, round(source.canvas.width * scale))
    expected_height = max(1, round(source.canvas.height * scale))
    if (canonical_hash(source.canvas) != binding.canvas_hash or
        source.canvas.source_sha256 != proxy.canvas.source_sha256 or
        source.canvas.space_id != proxy.canvas.space_id or
        (proxy.canvas.width, proxy.canvas.height) != (expected_width, expected_height) or
        (source.alpha is None) != (proxy.alpha is None) or
        source.color != proxy.color or proxy.color.profile_id != 'work_rec2020_gamma24_v1' or
        proxy.dtype != 'float32' or max(proxy.canvas.width, proxy.canvas.height) > 2048):
        raise ValueError('CAPTURE_PROXY_BINDING_INVALID')
    prefix = source.transform_chain.steps
    steps = proxy.transform_chain.steps
    if steps[:len(prefix)] != prefix:
        raise ValueError('CAPTURE_PROXY_TRANSFORM_INVALID')
    extra = steps[len(prefix):]
    if not extra:
        if source.canvas != proxy.canvas:
            raise ValueError('CAPTURE_PROXY_TRANSFORM_INVALID')
        return
    if (len(extra) != 1 or extra[0].kind != 'matrix' or
            extra[0].from_canvas != source.canvas or extra[0].to_canvas != proxy.canvas or
            extra[0].invertibility != 'invertible' or
            extra[0].valid_region.x != 0 or extra[0].valid_region.y != 0 or
            extra[0].valid_region.width != proxy.canvas.width or
            extra[0].valid_region.height != proxy.canvas.height):
        raise ValueError('CAPTURE_PROXY_TRANSFORM_INVALID')
    transform = extra[0]
    sx, sy = proxy.canvas.width / source.canvas.width, proxy.canvas.height / source.canvas.height
    expected = np.array([sx, 0, (sx - 1) / 2, 0, sy, (sy - 1) / 2, 0, 0, 1])
    if not np.allclose(transform.forward_matrix, expected, rtol=0, atol=1e-9):
        raise ValueError('CAPTURE_PROXY_TRANSFORM_INVALID')


def execute(ctx, artifacts: TaskArtifacts, pool, backend: str, evidence_provider=None):
    profile = ctx.lease.execution_profile
    if profile.get('renderer_version') != 'v2.2' or profile.get('backend') != backend:
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(CaptureAssessParams)
        if params.model_lock_sha != pool.lock.lock_id:
            raise ModelError('MODEL_LOCK_MISMATCH')
        source = artifacts.contract(params.binding.source, WorkingImage)
        proxy = artifacts.contract(params.proxy, WorkingImage)
        validate_proxy(source, proxy, params.binding)
        faces = artifacts.contract(params.faces, FaceObservations)
        calibration = artifacts.contract(params.policy.calibration, Calibration)
        context = artifacts.contract(params.context, CaptureContext)
        shots = artifacts.contract(context.shot_list, ShotList)
        artifacts.contract(context.consents, ConsentSnapshot)
        ctx.check_active()
        with ExitStack() as stack:
            local = stack.enter_context(artifacts.input(proxy.pixels, suffix='.npy'))
            work = stack.enter_context(read_array(local.path,
                (proxy.canvas.height, proxy.canvas.width, 3), 'float32',
                ctx.check_active, unit_interval=False))
            alpha = None
            if proxy.alpha is not None:
                local_alpha = stack.enter_context(artifacts.input(proxy.alpha, suffix='.npy'))
                alpha = stack.enter_context(read_alpha_array(local_alpha.path,
                    (proxy.canvas.height, proxy.canvas.width), ctx.check_active))
            # Keep sign for out-of-range clipping detection instead of
            # clamping negative samples before QA.
            linear = np.sign(work) * np.power(np.abs(work), np.float32(2.4))
            provider = evidence_provider or infer_face_evidence
            evidence = provider(ctx, work, faces, calibration, pool, backend, params.policy, alpha=alpha)
            report = assess_capture(linear, params.binding, faces, params.policy,
                calibration, context, shots, params.model_lock_sha,
                source.canvas.source_sha256, evidence)
        ctx.check_active()
        root = artifacts.stage_contract(report, 'document')
        return OperationResult(outputs=[root], bytes_written=len(canonical_bytes(report)))
    except ModelError as error:
        raise OperationFailure(str(error)) from error
    except ValueError as error:
        raise OperationFailure('INPUT_INVALID') from error


def capture_assess_operations(config: ArtifactConfig, pool, backend: str, evidence_provider=None):
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'document'})) as artifacts:
            return execute(ctx, artifacts, pool, backend, evidence_provider)
    return [Operation('capture.assess', 'gpu', run)]

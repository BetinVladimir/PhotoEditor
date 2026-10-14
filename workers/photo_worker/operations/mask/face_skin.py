"""Convert a bound Face19 label manifest into a conservative skin MaskRef."""
from contextlib import ExitStack
from collections.abc import Callable

import numpy as np

from photo_worker.algorithms.mask_png import png16_bound, write_png16
from photo_worker.algorithms.mask_png_verify import verify_png16
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.face_parse import FaceLabels19
from photo_worker.contracts.face_skin import FaceSkinParams
from photo_worker.contracts.images import MaskRef
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def _required_face_skin_ram(height: int, width: int) -> int:
    """Reserve probability, PNG verification and mapped-label working pages."""
    if type(height) is not int or type(width) is not int or height < 1 or width < 1:
        raise ValueError('FACE_SKIN_SIZE_INVALID')
    return 256 * 1024**2 + height * width * 64


def skin_probability(labels: np.ndarray, check_active: Callable[[], None] = lambda: None) -> np.ndarray:
    """Select Face19 class1 with bounded temporary memory and cancellation.

    The float32 output is caller-owned; comparisons never allocate a whole-frame
    boolean mask. Noncontiguous label views retain their exact row/column order.
    """
    check_active()
    if (labels.dtype != np.uint16 or labels.ndim != 2
            or min(labels.shape) < 1 or labels.size > 200000000):
        raise ValueError('Invalid Face19 label map')
    output = np.empty(labels.shape, dtype=np.float32)
    for row in range(labels.shape[0]):
        for start in range(0, labels.shape[1], 16384):
            check_active()
            block = labels[row, start:start + 16384]
            if np.any(block > 18):
                raise ValueError('Invalid Face19 label map')
            output[row, start:start + 16384] = block == 1
    check_active()
    return output


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile = ctx.lease.execution_profile
    estimate = profile.get('resource_estimate')
    ram = estimate.get('ram_bytes') if isinstance(estimate, dict) else None
    if profile.get('backend') != 'cpu' or type(ram) is not int or ram < 268435456:
        raise OperationFailure('CONFIG_INVALID')
    try:
        params = artifacts.params(FaceSkinParams)
        parsed = artifacts.contract(params.labels, FaceLabels19)
        if parsed.labels not in ctx.lease.command.inputs or parsed.canvas.width * parsed.canvas.height > 200000000:
            raise ValueError('Face skin input is undeclared or too large')
        if ram < _required_face_skin_ram(parsed.canvas.height, parsed.canvas.width):
            raise OperationFailure('RESOURCE_OOM')
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    h, w = parsed.canvas.height, parsed.canvas.width
    ctx.progress(0, 3)
    try:
        with ExitStack() as stack:
            local = stack.enter_context(artifacts.input(parsed.labels, suffix='.npy'))
            labels = stack.enter_context(read_array(local.path, (h, w), 'uint16', ctx.check_active))
            probability = skin_probability(np.asarray(labels), ctx.check_active)
            confidence = parsed.mean_confidence if np.any(probability) else 0.0
            bound = png16_bound(w, h)
            with artifacts.scratch.reserve(h * w * 4 + 8192 + bound) as reservation:
                path = reservation.path('face-skin', '.npy')
                mm = np.lib.format.open_memmap(path, mode='w+', dtype='<f4', shape=(h, w))
                try:
                    mm[:] = probability
                    mm.flush()
                    ctx.check_active()
                finally:
                    mm._mmap.close()
                png = reservation.path('face-skin', '.png')
                with artifacts.scratch.open_writer(png, reservation) as stream:
                    write_png16(probability, stream, max_bytes=bound, check_active=ctx.check_active)
                # Verify the materialized file rather than trusting the array
                # used to encode it. No output is staged before this gate.
                with read_array(path, (h, w), 'float32', ctx.check_active, unit_interval=True) as written_mask:
                    for row in range(h):
                        for start in range(0, w, 16384):
                            ctx.check_active()
                            if not np.array_equal(written_mask[row, start:start + 16384],
                                                  labels[row, start:start + 16384] == 1):
                                raise ValueError('Materialized skin mask differs from Face19 labels')
                    verify_png16(written_mask, png, check_active=ctx.check_active)
                reservation.validate()
                ctx.check_active()
                raw_ref = artifacts.stage(path, reservation, 'skin_mask', 'application/octet-stream')
                ctx.check_active()
                png_ref = artifacts.stage(png, reservation, 'skin_mask_png', 'image/png')
                written = path.stat().st_size + png.stat().st_size
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(2, 3)
    mask = MaskRef(artifact=raw_ref, canvas=parsed.canvas, transform_chain=parsed.transform_chain,
                   selector='skin', confidence=confidence, model_lock_id=parsed.model_lock_id, protected=False)
    ctx.check_active()
    manifest = artifacts.stage_contract(mask, 'skin_mask_ref')
    ctx.progress(3, 3)
    return OperationResult(outputs=[raw_ref, png_ref, manifest], review_required=confidence < .75,
                           bytes_written=written + len(canonical_bytes(mask)))


def face_skin_operations(config: ArtifactConfig) -> list[Operation]:
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'skin_mask', 'skin_mask_png', 'skin_mask_ref'})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation('mask.face_skin', 'cpu', run)]


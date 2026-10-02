"""Convert a bound Face19 label manifest into a conservative skin MaskRef."""
from contextlib import ExitStack

import numpy as np

from photo_worker.algorithms.mask_png import png16_bound, write_png16
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.face_parse import FaceLabels19
from photo_worker.contracts.face_skin import FaceSkinParams
from photo_worker.contracts.images import MaskRef
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def skin_probability(labels: np.ndarray) -> np.ndarray:
    """Return the Face19 skin class only; facial features remain protected by exclusion."""
    if labels.dtype != np.uint16 or labels.ndim != 2 or np.any(labels > 18):
        raise ValueError('Invalid Face19 label map')
    return (labels == 1).astype(np.float32)


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
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    h, w = parsed.canvas.height, parsed.canvas.width
    ctx.progress(0, 3)
    try:
        with ExitStack() as stack:
            local = stack.enter_context(artifacts.input(parsed.labels, suffix='.npy'))
            labels = stack.enter_context(read_array(local.path, (h, w), 'uint16', ctx.check_active))
            probability = skin_probability(np.asarray(labels))
            confidence = parsed.mean_confidence if np.any(probability) else 0.0
            bound = png16_bound(w, h)
            with artifacts.scratch.reserve(h * w * 4 + 8192 + bound) as reservation:
                path = reservation.path('face-skin', '.npy')
                mm = np.lib.format.open_memmap(path, mode='w+', dtype='<f4', shape=(h, w))
                mm[:] = probability
                mm.flush()
                mm._mmap.close()
                png = reservation.path('face-skin', '.png')
                with artifacts.scratch.open_writer(png, reservation) as stream:
                    write_png16(probability, stream, max_bytes=bound, check_active=ctx.check_active)
                reservation.validate()
                raw_ref = artifacts.stage(path, reservation, 'skin_mask', 'application/octet-stream')
                png_ref = artifacts.stage(png, reservation, 'skin_mask_png', 'image/png')
                written = path.stat().st_size + png.stat().st_size
    except ValueError as exc:
        raise OperationFailure('INPUT_INVALID') from exc
    ctx.progress(2, 3)
    mask = MaskRef(artifact=raw_ref, canvas=parsed.canvas, transform_chain=parsed.transform_chain,
                   selector='skin', confidence=confidence, model_lock_id=parsed.model_lock_id, protected=False)
    manifest = artifacts.stage_contract(mask, 'skin_mask_ref')
    ctx.progress(3, 3)
    return OperationResult(outputs=[raw_ref, png_ref, manifest], review_required=confidence < .75,
                           bytes_written=written + len(canonical_bytes(mask)))


def face_skin_operations(config: ArtifactConfig) -> list[Operation]:
    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({'skin_mask', 'skin_mask_png', 'skin_mask_ref'})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation('mask.face_skin', 'cpu', run)]


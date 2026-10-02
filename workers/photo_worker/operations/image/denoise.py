"""SCUNet denoise adapter with bounded halo tiles and wide-gamut delta transfer."""
from __future__ import annotations

from contextlib import ExitStack
from typing import Protocol
import numpy as np

from photo_worker.algorithms.model_delta import transfer_model_delta, work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.restoration import DenoiseParams, DenoiseReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


class DenoiseEngine(Protocol):
    def infer(self, rgb: np.ndarray) -> np.ndarray: ...


def _infer(engine: DenoiseEngine, value: np.ndarray) -> np.ndarray:
    result = np.asarray(engine.infer(value), np.float32)
    if result.shape != value.shape or not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1):
        raise ValueError("MODEL_OUTPUT_INVALID")
    return result


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts, pool: ModelPool, backend: str) -> OperationResult:
    profile = ctx.lease.execution_profile; estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != backend or backend not in ("cpu", "cuda", "mps") or type(ram) is not int or ram < 1_073_741_824:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(DenoiseParams)
        if params.model_lock_id != pool.lock.lock_id: raise ModelError("MODEL_LOCK_MISMATCH")
        image = artifacts.contract(params.image, WorkingImage)
        if image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1" or image.color.encoding != "gamma24":
            raise ValueError("DENOISE_IMAGE_INVALID")
        if image.pixels not in ctx.lease.command.inputs or image.alpha is not None and image.alpha not in ctx.lease.command.inputs:
            raise ValueError("DENOISE_INPUT_UNDECLARED")
        height, width = image.canvas.height, image.canvas.width
        if height * width > 200_000_000: raise ValueError("DENOISE_PIXEL_LIMIT")
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc
    count_y = (height + params.tile - 1) // params.tile; count_x = (width + params.tile - 1) // params.tile
    total = count_y * count_x + 2; ctx.progress(0, total)
    source_clip = gamut_excess = disabled = 0; done = 0
    try:
        with pool.acquire(params.model_id, backend) as engine, ExitStack() as stack:
            local = stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
            source = stack.enter_context(read_array(local.path, (height, width, 3), "float32", ctx.check_active, unit_interval=True))
            with artifacts.scratch.reserve(height * width * 12 + 16384) as reservation:
                output_path = reservation.path("denoised-rgb", ".npy")
                output = np.lib.format.open_memmap(output_path, mode="w+", dtype="<f4", shape=(height, width, 3))
                try:
                    for y in range(0, height, params.tile):
                        for x in range(0, width, params.tile):
                            ctx.check_active(); y1 = min(y + params.tile, height); x1 = min(x + params.tile, width)
                            hy0 = max(0, y - params.overlap); hx0 = max(0, x - params.overlap)
                            hy1 = min(height, y1 + params.overlap); hx1 = min(width, x1 + params.overlap)
                            work = np.asarray(source[hy0:hy1, hx0:hx1], np.float32)
                            model_input, _ = work_to_model(work)
                            model_output = _infer(engine, model_input)
                            restored, evidence = transfer_model_delta(work, model_input, model_output, params.strength)
                            core = (slice(y - hy0, y1 - hy0), slice(x - hx0, x1 - hx0))
                            output[y:y1, x:x1] = restored[core]
                            source_clip += int(np.count_nonzero(evidence.source_clipped[core]))
                            gamut_excess += int(np.count_nonzero(evidence.gamut_excess[core]))
                            disabled += int(np.count_nonzero(evidence.disabled[core]))
                            done += 1; ctx.progress(done, total)
                    output.flush(); reservation.validate()
                finally:
                    output._mmap.close()
                pixel_ref = artifacts.stage(output_path, reservation, "working_rgb", "application/octet-stream")
                written = output_path.stat().st_size
    except ModelError as exc:
        raise OperationFailure(str(exc)) from exc
    except ValueError as exc:
        raise OperationFailure("MODEL_OUTPUT_INVALID") from exc
    output_image = image.model_copy(update={"pixels": pixel_ref})
    pixels = height * width; warnings = []
    if source_clip: warnings.append("source_srgb_clipped")
    if gamut_excess: warnings.append("delta_gamut_excess")
    report = DenoiseReport(image=params.image, model_id=params.model_id, model_lock_id=params.model_lock_id,
        backend=backend, color_adapter_id=params.color_adapter_id, strength=params.strength, tile=params.tile,
        overlap=params.overlap, pixel_count=pixels, disabled_delta_fraction=disabled / pixels,
        source_clip_fraction=source_clip / pixels, gamut_excess_fraction=gamut_excess / pixels,
        alpha_preserved=image.alpha is not None, warnings=warnings)
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "denoise_report"); ctx.progress(total, total)
    return OperationResult(outputs=[pixel_ref, image_ref, report_ref], review_required=bool(warnings),
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def denoise_operations(config: ArtifactConfig, pool: ModelPool, backend: str) -> list[Operation]:
    def denoise(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_image", "denoise_report"})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation("image.denoise", "gpu", denoise)]

"""SCUNet denoise adapter with bounded halo tiles and wide-gamut delta transfer."""
from __future__ import annotations

from contextlib import ExitStack
from typing import Protocol
import numpy as np

from .prepare_denoise import prepare_denoise,infer as _infer
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
            with prepare_denoise(source,params,engine,artifacts.scratch,check_active=ctx.check_active,
                    progress=ctx.progress) as prepared:
                pixel_ref=artifacts.stage(prepared.path,prepared.reservation,'working_rgb','application/octet-stream')
                written=prepared.path.stat().st_size
                source_clip,gamut_excess,disabled=prepared.source_clip,prepared.gamut_excess,prepared.disabled
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

"""Real-ESRGAN adapter; model detail is applied as a gamut-safe delta over linear resize."""
from __future__ import annotations

from contextlib import ExitStack
import cv2
import numpy as np

from .prepare_upscale import prepare_upscale,upscale_geometry,infer as _infer
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import Canvas, Rect, ResizeTransform, TransformChain, WorkingImage
from photo_worker.contracts.restoration import UpscaleParams, UpscaleReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def _required_upscale_ram(height: int, width: int, scale: int, has_alpha: bool) -> int:
    """Reserve native tile/model headroom plus the full-frame alpha resize temporaries."""
    if (type(height) is not int or type(width) is not int or type(scale) is not int
            or type(has_alpha) is not bool or height < 1 or width < 1 or scale < 1):
        raise ValueError("UPSCALE_SIZE_INVALID")
    pixels = height * width * scale * scale
    return 768 * 1024**2 + (pixels * 8 if has_alpha else 0)


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts, pool: ModelPool, backend: str) -> OperationResult:
    profile = ctx.lease.execution_profile; estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != backend or backend not in ("cpu", "cuda", "mps") or type(ram) is not int or ram < 1_073_741_824:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(UpscaleParams)
        if params.model_lock_id != pool.lock.lock_id: raise ModelError("MODEL_LOCK_MISMATCH")
        image = artifacts.contract(params.image, WorkingImage)
        if image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1" or image.color.encoding != "gamma24":
            raise ValueError("UPSCALE_IMAGE_INVALID")
        required = [image.pixels, *([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required): raise ValueError("UPSCALE_INPUT_UNDECLARED")
        height, width = image.canvas.height, image.canvas.width; oh, ow = height * params.scale, width * params.scale
        if oh * ow > 200_000_000: raise ValueError("UPSCALE_PIXEL_LIMIT")
        if ram < _required_upscale_ram(height, width, params.scale, image.alpha is not None):
            raise OperationFailure("RESOURCE_OOM")
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("INPUT_INVALID") from exc
    tiles = ((height + 255) // 256) * ((width + 255) // 256); total = tiles + 3; ctx.progress(0, total)
    source_clip = gamut_excess = disabled = done = 0
    try:
        with pool.acquire(params.model_id, backend) as engine, ExitStack() as stack:
            local = stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
            source = stack.enter_context(read_array(local.path, (height, width, 3), "float32", ctx.check_active, unit_interval=True))
            alpha = None
            if image.alpha:
                alpha_local = stack.enter_context(artifacts.input(image.alpha, suffix=".npy"))
                alpha = stack.enter_context(read_array(alpha_local.path, (height, width), "uint16", ctx.check_active))
            with prepare_upscale(source,alpha,params,engine,artifacts.scratch,check_active=ctx.check_active,
                    progress=ctx.progress) as prepared:
                pixel_ref=artifacts.stage(prepared.rgb_path,prepared.reservation,'working_rgb','application/octet-stream')
                alpha_ref=artifacts.stage(prepared.alpha_path,prepared.reservation,'working_alpha','application/octet-stream') if prepared.alpha_path else None
                written=prepared.rgb_path.stat().st_size+(prepared.alpha_path.stat().st_size if prepared.alpha_path else 0)
                source_clip,gamut_excess,disabled=prepared.source_clip,prepared.gamut_excess,prepared.disabled
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("MODEL_OUTPUT_INVALID") from exc
    canvas,chain=upscale_geometry(image,params)
    output_image = WorkingImage(pixels=pixel_ref, canvas=canvas, color=image.color, dtype="float32", alpha=alpha_ref, transform_chain=chain)
    pixels = oh * ow; warnings = []
    if source_clip: warnings.append("source_srgb_clipped")
    if gamut_excess: warnings.append("delta_gamut_excess")
    report = UpscaleReport(image=params.image, model_id=params.model_id, model_lock_id=params.model_lock_id, backend=backend,
        scale=params.scale, color_adapter_id=params.color_adapter_id, tile=256, overlap=32,
        input_pixel_count=height*width, output_pixel_count=pixels, disabled_delta_fraction=disabled/pixels,
        source_clip_fraction=source_clip/pixels, gamut_excess_fraction=gamut_excess/pixels,
        alpha_resampled=alpha_ref is not None, warnings=warnings)
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "upscale_report"); ctx.progress(total, total)
    return OperationResult(outputs=[pixel_ref, *([alpha_ref] if alpha_ref else []), image_ref, report_ref], review_required=bool(warnings),
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def upscale_operations(config: ArtifactConfig, pool: ModelPool, backend: str) -> list[Operation]:
    def upscale(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_alpha", "working_image", "upscale_report"})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation("image.upscale", "gpu", upscale)]

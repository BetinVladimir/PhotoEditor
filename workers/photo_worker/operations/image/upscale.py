"""Real-ESRGAN adapter; model detail is applied as a gamut-safe delta over linear resize."""
from __future__ import annotations

from contextlib import ExitStack
import cv2
import numpy as np

from photo_worker.algorithms.model_delta import resize_work_linear, transfer_model_delta, work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import Canvas, Rect, ResizeTransform, TransformChain, WorkingImage
from photo_worker.contracts.restoration import UpscaleParams, UpscaleReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def _infer(engine, value, scale):
    result = np.asarray(engine.infer(value), np.float32)
    expected = (value.shape[0] * scale, value.shape[1] * scale, 3)
    if result.shape != expected or not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1):
        raise ValueError("MODEL_OUTPUT_INVALID")
    return result


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
            reserve_bytes = oh * ow * (12 + (2 if alpha is not None else 0)) + 32768
            with artifacts.scratch.reserve(reserve_bytes) as reservation:
                rgb_path = reservation.path("upscaled-rgb", ".npy")
                output = np.lib.format.open_memmap(rgb_path, mode="w+", dtype="<f4", shape=(oh, ow, 3))
                alpha_path = reservation.path("upscaled-alpha", ".npy") if alpha is not None else None
                out_alpha = np.lib.format.open_memmap(alpha_path, mode="w+", dtype="<u2", shape=(oh, ow)) if alpha_path else None
                try:
                    for y in range(0, height, 256):
                        for x in range(0, width, 256):
                            ctx.check_active(); y1 = min(y + 256, height); x1 = min(x + 256, width)
                            hy0 = max(0, y - 32); hx0 = max(0, x - 32); hy1 = min(height, y1 + 32); hx1 = min(width, x1 + 32)
                            work = np.asarray(source[hy0:hy1, hx0:hx1], np.float32); model_in, _ = work_to_model(work)
                            model_out = _infer(engine, model_in, params.scale)
                            base_work = resize_work_linear(work, (work.shape[1] * params.scale, work.shape[0] * params.scale))
                            model_base = cv2.resize(model_in, (model_out.shape[1], model_out.shape[0]), interpolation=cv2.INTER_CUBIC)
                            model_base = np.clip(model_base, 0, 1).astype(np.float32)
                            restored, evidence = transfer_model_delta(base_work, model_base, model_out, 1.)
                            cy0 = (y - hy0) * params.scale; cx0 = (x - hx0) * params.scale
                            cy1 = cy0 + (y1 - y) * params.scale; cx1 = cx0 + (x1 - x) * params.scale
                            core = (slice(cy0, cy1), slice(cx0, cx1)); oy = slice(y * params.scale, y1 * params.scale); ox = slice(x * params.scale, x1 * params.scale)
                            output[oy, ox] = restored[core]
                            source_clip += int(np.count_nonzero(evidence.source_clipped[core])); gamut_excess += int(np.count_nonzero(evidence.gamut_excess[core])); disabled += int(np.count_nonzero(evidence.disabled[core]))
                            done += 1; ctx.progress(done, total)
                    if out_alpha is not None:
                        out_alpha[:] = cv2.resize(np.asarray(alpha), (ow, oh), interpolation=cv2.INTER_LINEAR).astype(np.uint16)
                        out_alpha.flush()
                    output.flush(); reservation.validate()
                finally:
                    output._mmap.close()
                    if out_alpha is not None: out_alpha._mmap.close()
                pixel_ref = artifacts.stage(rgb_path, reservation, "working_rgb", "application/octet-stream")
                alpha_ref = artifacts.stage(alpha_path, reservation, "working_alpha", "application/octet-stream") if alpha_path else None
                written = rgb_path.stat().st_size + (alpha_path.stat().st_size if alpha_path else 0)
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("MODEL_OUTPUT_INVALID") from exc
    canvas = Canvas(width=ow, height=oh, orientation=1, space_id=f"up{params.scale}-{image.canvas.source_sha256[:16]}", source_sha256=image.canvas.source_sha256)
    step = ResizeTransform(kind="resize", from_canvas=image.canvas, to_canvas=canvas, invertibility="invertible",
                           valid_region=Rect(x=0, y=0, width=ow, height=oh), pixel_centers="half_pixel")
    chain = TransformChain.create([*image.transform_chain.steps, step])
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

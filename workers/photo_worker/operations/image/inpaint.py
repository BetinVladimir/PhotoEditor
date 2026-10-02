"""Confirmed-mask Big-LaMa candidate adapter with exact outside/protected restoration."""
from __future__ import annotations

from contextlib import ExitStack
import cv2
import numpy as np

from photo_worker.algorithms.model_delta import transfer_model_delta, work_to_model
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.restoration import InpaintParams, InpaintReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def _infer(engine, rgb, mask):
    h, w = rgb.shape[:2]; ph = (-h) % 8; pw = (-w) % 8
    image = np.pad(rgb, ((0, ph), (0, pw), (0, 0)), mode="reflect")
    binary = np.pad(mask.astype(np.float32), ((0, ph), (0, pw)), mode="constant")
    result = np.asarray(engine.infer(image, binary), np.float32)
    if result.shape != image.shape or not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1):
        raise ValueError("MODEL_OUTPUT_INVALID")
    return result[:h, :w]


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts, pool: ModelPool, backend: str) -> OperationResult:
    profile = ctx.lease.execution_profile; estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != backend or backend not in ("cpu", "cuda", "mps") or type(ram) is not int or ram < 1_073_741_824:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(InpaintParams)
        if params.model_lock_id != pool.lock.lock_id: raise ModelError("MODEL_LOCK_MISMATCH")
        image = artifacts.contract(params.image, WorkingImage)
        if image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1": raise ValueError("INPAINT_IMAGE_INVALID")
        masks = [params.mask, *params.protected]
        if any(mask.canvas != image.canvas or mask.transform_chain != image.transform_chain for mask in masks): raise ValueError("INPAINT_MASK_GEOMETRY")
        required = [image.pixels, params.mask.artifact, *[m.artifact for m in params.protected], *([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required): raise ValueError("INPAINT_INPUT_UNDECLARED")
        h, w = image.canvas.height, image.canvas.width
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("INPUT_INVALID") from exc
    ctx.progress(0, 5)
    try:
        with pool.acquire(params.model_id, backend) as engine, ExitStack() as stack:
            def mapped(ref, shape):
                local = stack.enter_context(artifacts.input(ref, suffix=".npy"))
                return stack.enter_context(read_array(local.path, shape, "float32", ctx.check_active, unit_interval=True))
            source = mapped(image.pixels, (h,w,3)); target = np.asarray(mapped(params.mask.artifact, (h,w))) >= .5
            protected = np.zeros((h, w), bool)
            for mask in params.protected: protected |= np.asarray(mapped(mask.artifact, (h,w))) >= .5
            effective = target & ~protected
            if not effective.any(): raise ValueError("INPAINT_MASK_EMPTY")
            ys, xs = np.nonzero(effective); x0 = max(0, int(xs.min()) - params.context_pixels); x1 = min(w, int(xs.max()) + 1 + params.context_pixels)
            y0 = max(0, int(ys.min()) - params.context_pixels); y1 = min(h, int(ys.max()) + 1 + params.context_pixels)
            if (x1-x0)*(y1-y0) > min(32_000_000, ram//64): raise OperationFailure("RESOURCE_OOM")
            ctx.progress(1, 5); global_pass = max(x1-x0, y1-y0) > 1024
            roi_work = np.asarray(source[y0:y1, x0:x1], np.float32); roi_mask = effective[y0:y1, x0:x1]
            model_input, _ = work_to_model(roi_work)
            if global_pass:
                scale = 1024 / max(h, w); sw, sh = max(8, int(round(w*scale))), max(8, int(round(h*scale)))
                preview_work = cv2.resize(np.asarray(source), (sw, sh), interpolation=cv2.INTER_AREA)
                preview_mask = cv2.resize(effective.astype(np.uint8), (sw, sh), interpolation=cv2.INTER_NEAREST) > 0
                preview_input, _ = work_to_model(preview_work); preview_result = _infer(engine, preview_input, preview_mask)
                global_result = cv2.resize(preview_result, (w, h), interpolation=cv2.INTER_CUBIC)
                provisional = np.clip(global_result[y0:y1, x0:x1], 0, 1).astype(np.float32)
                model_input = np.where(roi_mask[...,None], provisional, model_input)
            model_output = _infer(engine, model_input, roi_mask); ctx.progress(2, 5)
            restored, _ = transfer_model_delta(roi_work, model_input, model_output, 1.)
            distance = cv2.distanceTransform(roi_mask.astype(np.uint8), cv2.DIST_L2, 3)
            weight = np.minimum(distance / params.feather_pixels, 1).astype(np.float32)
            weight[protected[y0:y1, x0:x1]] = 0
            with artifacts.scratch.reserve(h*w*12 + 16384) as reservation:
                path = reservation.path("inpaint-rgb", ".npy")
                output = np.lib.format.open_memmap(path, mode="w+", dtype="<f4", shape=(h, w, 3))
                try:
                    for row in range(0, h, 256): ctx.check_active(); output[row:row+256] = source[row:row+256]
                    original = np.asarray(source[y0:y1, x0:x1], np.float32)
                    output[y0:y1, x0:x1] = original + (restored-original)*weight[...,None]
                    output.flush(); reservation.validate()
                finally: output._mmap.close()
                pixel_ref = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream"); written = path.stat().st_size
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("MODEL_OUTPUT_INVALID") from exc
    ctx.progress(3, 5)
    output_image = image.model_copy(update={"pixels": pixel_ref})
    report = InpaintReport(image=params.image, mask=params.mask.artifact, model_id=params.model_id,
        model_lock_id=params.model_lock_id, backend=backend, changed_pixels=int(np.count_nonzero(effective)),
        target_pixels=int(np.count_nonzero(target)), protected_pixels=int(np.count_nonzero(protected)),
        global_pass=global_pass, roi=[x0,y0,x1-x0,y1-y0], feather_pixels=params.feather_pixels,
        alpha_preserved=image.alpha is not None)
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(4, 5)
    report_ref = artifacts.stage_contract(report, "inpaint_report"); ctx.progress(5, 5)
    return OperationResult(outputs=[pixel_ref,image_ref,report_ref], review_required=True,
        bytes_written=written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))


def inpaint_operations(config: ArtifactConfig, pool: ModelPool, backend: str) -> list[Operation]:
    def inpaint(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb","working_image","inpaint_report"})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation("image.inpaint", "gpu", inpaint)]

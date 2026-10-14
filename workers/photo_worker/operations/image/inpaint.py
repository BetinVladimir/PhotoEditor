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


def _infer(engine, rgb, mask, max_padded_pixels):
    h, w = rgb.shape[:2]; ph = (-h) % 8; pw = (-w) % 8
    if (type(max_padded_pixels) is not int or max_padded_pixels < 1
            or (h + ph) * (w + pw) > max_padded_pixels):
        raise OperationFailure("RESOURCE_OOM")
    image = np.pad(rgb, ((0, ph), (0, pw), (0, 0)), mode="reflect")
    binary = np.pad(mask.astype(np.float32), ((0, ph), (0, pw)), mode="constant")
    result = np.asarray(engine.infer(image, binary), np.float32)
    if result.shape != image.shape or not np.isfinite(result).all() or np.any(result < 0) or np.any(result > 1):
        raise ValueError("MODEL_OUTPUT_INVALID")
    return result[:h, :w]


def _mask_roi(mask, context_pixels):
    """Find the selected bounding box without allocating per-pixel coordinates."""
    if (mask.dtype != np.bool_ or mask.ndim != 2 or mask.size == 0
            or type(context_pixels) is not int or context_pixels < 0):
        raise ValueError("INPAINT_MASK_INVALID")
    rows = np.any(mask, axis=1)
    cols = np.any(mask, axis=0)
    if not np.any(rows):
        raise ValueError("INPAINT_MASK_EMPTY")
    h, w = mask.shape
    y0 = max(0, int(np.argmax(rows)) - context_pixels)
    y1 = min(h, h - int(np.argmax(rows[::-1])) + context_pixels)
    x0 = max(0, int(np.argmax(cols)) - context_pixels)
    x1 = min(w, w - int(np.argmax(cols[::-1])) + context_pixels)
    return x0, y0, x1, y1


def _required_frame_ram(height, width):
    if type(height) is not int or type(width) is not int or height < 1 or width < 1:
        raise ValueError("INPAINT_IMAGE_INVALID")
    return 512 * 1024**2 + height * width * 32


def _roi_pixel_budget(ram, height, width):
    """Reserve full-frame masks, then charge bounded model/delta work per ROI pixel."""
    if type(ram) is not int or ram < 0:
        raise ValueError("INPAINT_RESOURCE_INVALID")
    remaining = ram - _required_frame_ram(height, width)
    return min(32_000_000, max(0, remaining // 192))


def _blend_selected(output, original, restored, weight, x0, y0, check_active):
    """Write bounded tiles; zero-weight source pixels keep their exact bits."""
    if (original.dtype != np.float32 or restored.dtype != np.float32
            or weight.dtype != np.float32 or original.shape != restored.shape
            or original.shape[:2] != weight.shape or original.ndim != 3
            or original.shape[2] != 3 or not callable(check_active)):
        raise ValueError("INPAINT_BLEND_INVALID")
    height, width = weight.shape
    changed_pixels = 0
    for y in range(0, height, 256):
        for x in range(0, width, 256):
            check_active()
            ys = slice(y, min(y + 256, height))
            xs = slice(x, min(x + 256, width))
            strength = weight[ys, xs]
            selected = strength > 0
            if not np.any(selected):
                continue
            base = original[ys, xs]
            repair = restored[ys, xs]
            target = output[y0+y:y0+ys.stop, x0+x:x0+xs.stop]
            before = base[selected]
            after = (before + (repair[selected] - before) *
                     strength[selected][:, None])
            changed_pixels += int(np.count_nonzero(np.any(
                after.view(np.uint32) != before.view(np.uint32), axis=1)))
            target[selected] = after
    check_active()
    return changed_pixels


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
        if ram < _required_frame_ram(h, w):
            raise OperationFailure("RESOURCE_OOM")
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
            x0, y0, x1, y1 = _mask_roi(effective, params.context_pixels)
            padded_budget = _roi_pixel_budget(ram, h, w)
            if (x1-x0)*(y1-y0) > padded_budget: raise OperationFailure("RESOURCE_OOM")
            ctx.progress(1, 5); global_pass = max(x1-x0, y1-y0) > 1024
            roi_work = np.asarray(source[y0:y1, x0:x1], np.float32); roi_mask = effective[y0:y1, x0:x1]
            model_input, _ = work_to_model(roi_work)
            if global_pass:
                scale = 1024 / max(h, w); sw, sh = max(8, int(round(w*scale))), max(8, int(round(h*scale)))
                preview_work = cv2.resize(np.asarray(source), (sw, sh), interpolation=cv2.INTER_AREA)
                preview_mask = cv2.resize(effective.astype(np.uint8), (sw, sh), interpolation=cv2.INTER_NEAREST) > 0
                preview_input, _ = work_to_model(preview_work); preview_result = _infer(engine, preview_input, preview_mask, padded_budget)
                global_result = cv2.resize(preview_result, (w, h), interpolation=cv2.INTER_CUBIC)
                provisional = np.clip(global_result[y0:y1, x0:x1], 0, 1).astype(np.float32)
                model_input = np.where(roi_mask[...,None], provisional, model_input)
            model_output = _infer(engine, model_input, roi_mask, padded_budget); ctx.progress(2, 5)
            restored, _ = transfer_model_delta(roi_work, model_input, model_output, 1.)
            distance = cv2.distanceTransform(roi_mask.astype(np.uint8), cv2.DIST_L2, 3)
            weight = np.minimum(distance / params.feather_pixels, 1).astype(np.float32)
            weight[protected[y0:y1, x0:x1]] = 0
            with artifacts.scratch.reserve(h*w*12 + 16384) as reservation:
                path = reservation.path("inpaint-rgb", ".npy")
                output = np.lib.format.open_memmap(path, mode="w+", dtype="<f4", shape=(h, w, 3))
                try:
                    for row in range(0, h, 256): ctx.check_active(); output[row:row+256] = source[row:row+256]
                    changed_pixels = _blend_selected(output, roi_work, restored,
                                                     weight, x0, y0,
                                                     ctx.check_active)
                    output.flush(); reservation.validate(); ctx.check_active()
                finally: output._mmap.close()
                ctx.check_active()
                pixel_ref = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream"); written = path.stat().st_size
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("MODEL_OUTPUT_INVALID") from exc
    ctx.progress(3, 5)
    output_image = image.model_copy(update={"pixels": pixel_ref})
    report = InpaintReport(image=params.image, mask=params.mask.artifact, model_id=params.model_id,
        model_lock_id=params.model_lock_id, backend=backend, changed_pixels=changed_pixels,
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

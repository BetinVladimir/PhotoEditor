"""Confirmed-mask Big-LaMa candidate adapter with exact outside/protected restoration."""
from __future__ import annotations
from .generated_coverage import stage_generated_coverage

from contextlib import ExitStack
import cv2
import numpy as np

from photo_worker.operations.semantic.candidate_blend import blend_candidate_region as _blend_selected
from photo_worker.operations.semantic.candidate_preservation import verify_candidate_preservation
from photo_worker.operations.semantic.candidate_inpaint import (
    infer_inpaint_candidate as _infer,
    compute_inpaint_region,
    inpaint_mask_roi as _mask_roi,
)
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.restoration import InpaintParams, InpaintReport
from photo_worker.models.install import ModelError
from photo_worker.models.pool import ModelPool
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


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
            for mask in params.protected: protected |= np.asarray(mapped(mask.artifact, (h,w))) > 0
            effective = target & ~protected
            x0, y0, x1, y1 = _mask_roi(effective, params.context_pixels)
            padded_budget = _roi_pixel_budget(ram, h, w)
            if (x1-x0)*(y1-y0) > padded_budget: raise OperationFailure("RESOURCE_OOM")
            ctx.progress(1, 5); global_pass = max(x1-x0, y1-y0) > 1024
            roi_work = np.asarray(source[y0:y1, x0:x1], np.float32); roi_mask = effective[y0:y1, x0:x1]
            restored, weight, global_pass = compute_inpaint_region(
                engine, source, effective, protected, (x0, y0, x1, y1),
                feather_pixels=params.feather_pixels, max_padded_pixels=padded_budget,
                check_active=ctx.check_active)
            ctx.progress(2, 5)
            with artifacts.scratch.reserve(h*w*12 + 16384) as reservation:
                path = reservation.path("inpaint-rgb", ".npy")
                output = np.lib.format.open_memmap(path, mode="w+", dtype="<f4", shape=(h, w, 3))
                try:
                    for row in range(0, h, 256): ctx.check_active(); output[row:row+256] = source[row:row+256]
                    changed_pixels = _blend_selected(output, roi_work, restored,
                                                     weight, x0, y0,
                                                     ctx.check_active)
                    # Independent full-canvas proof before any artifact staging.
                    # The fixed 512 MiB frame allowance includes this bounded
                    # 8 MiB verification workspace; no full float mask copies.
                    proof = verify_candidate_preservation(source, output, target, protected,
                        max_workspace_bytes=8*1048576, check_active=ctx.check_active)
                    if proof.changed_pixels != changed_pixels:
                        raise ValueError("INPAINT_CHANGED_PIXELS_MISMATCH")
                    output.flush(); reservation.validate(); ctx.check_active()
                finally: output._mmap.close()
                ctx.check_active()
                pixel_ref = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream"); written = path.stat().st_size
    except ModelError as exc: raise OperationFailure(str(exc)) from exc
    except ValueError as exc: raise OperationFailure("MODEL_OUTPUT_INVALID") from exc
    ctx.progress(3, 5)
    output_image = image.model_copy(update={"pixels": pixel_ref})
    generated_pixels_ref, generated_mask_ref, generated_written = stage_generated_coverage(artifacts, output_image, effective, ctx.check_active)
    report = InpaintReport(generated_mask=generated_mask_ref,image=params.image, mask=params.mask.artifact, model_id=params.model_id,
        model_lock_id=params.model_lock_id, backend=backend, changed_pixels=changed_pixels,
        target_pixels=int(np.count_nonzero(target)), protected_pixels=int(np.count_nonzero(protected)),
        global_pass=global_pass, roi=[x0,y0,x1-x0,y1-y0], feather_pixels=params.feather_pixels,
        alpha_preserved=image.alpha is not None)
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(4, 5)
    report_ref = artifacts.stage_contract(report, "inpaint_report"); ctx.progress(5, 5)
    return OperationResult(outputs=[generated_pixels_ref,generated_mask_ref,pixel_ref,image_ref,report_ref], review_required=True,
        bytes_written=generated_written+written+len(canonical_bytes(output_image))+len(canonical_bytes(report)))


def inpaint_operations(config: ArtifactConfig, pool: ModelPool, backend: str) -> list[Operation]:
    def inpaint(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb","working_image","inpaint_report","generated_pixels","generated_mask"})) as artifacts:
            return execute(ctx, artifacts, pool, backend)
    return [Operation("image.inpaint", "gpu", inpaint)]

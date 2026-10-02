"""Compose a confirmed subject over transparency or a solid background."""
from __future__ import annotations

from contextlib import ExitStack
import numpy as np

from photo_worker.algorithms.background import (alpha_u16, asset_geometry, contact_shadow,
                                                effective_alpha, render_asset_tile, solid, transparent)
from photo_worker.contracts.background import ComposeParams, ComposeReport
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile = ctx.lease.execution_profile
    estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != "cpu" or type(ram) is not int or ram < 536_870_912:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(ComposeParams)
        image = artifacts.contract(params.image, WorkingImage)
        if (image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1"
                or params.subject.canvas != image.canvas or params.subject.transform_chain != image.transform_chain
                or image.canvas.width * image.canvas.height > 200_000_000):
            raise ValueError("BACKGROUND_GEOMETRY_INVALID")
        if params.subject.confidence < .75:
            raise OperationFailure("MASK_UNRELIABLE")
        background = artifacts.contract(params.background, WorkingImage) if params.background else None
        if background is not None and (background.dtype != "float32" or background.color != image.color or background.alpha is not None):
            raise ValueError("BACKGROUND_ASSET_INVALID")
        geometry = asset_geometry(background.canvas.width, background.canvas.height, image.canvas.width, image.canvas.height,
                                  params.asset_scale, params.position_x, params.position_y) if background else None
        required = [image.pixels, params.subject.artifact, *([image.alpha] if image.alpha else []),
                    *([background.pixels] if background else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):
            raise ValueError("BACKGROUND_INPUT_UNDECLARED")
    except OperationFailure:
        raise
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc

    height, width, tile = image.canvas.height, image.canvas.width, params.tile
    tiles = ((height + tile - 1) // tile) * ((width + tile - 1) // tile)
    total = tiles + 2
    ctx.progress(0, total)
    with ExitStack() as stack:
        def mapped(ref, shape, dtype, unit=False):
            local = stack.enter_context(artifacts.input(ref, suffix=".npy"))
            return stack.enter_context(read_array(local.path, shape, dtype, ctx.check_active, unit_interval=unit))
        rgb = mapped(image.pixels, (height, width, 3), "float32", True)
        subject = mapped(params.subject.artifact, (height, width), "float32", True)
        source_alpha = mapped(image.alpha, (height, width), "uint16") if image.alpha else None
        background_rgb = mapped(background.pixels, (background.canvas.height, background.canvas.width, 3), "float32", True) if background else None
        shadow_radius = int(np.ceil(params.shadow_blur_fraction * max(height, width)))
        shadow_dx = int(round(params.shadow_offset_x * width)); shadow_dy = int(round(params.shadow_offset_y * height))
        shadow_halo = shadow_radius + max(abs(shadow_dx), abs(shadow_dy)) if params.shadow_opacity else 0
        output_bytes = height * width * (14 if params.mode == "transparent" else 12) + 16_384
        with artifacts.scratch.reserve(output_bytes) as reservation:
            rgb_path = reservation.path("background-rgb", ".npy")
            alpha_path = reservation.path("background-alpha", ".npy") if params.mode == "transparent" else None
            rgb_out = np.lib.format.open_memmap(rgb_path, mode="w+", dtype="<f4", shape=(height, width, 3))
            alpha_out = np.lib.format.open_memmap(alpha_path, mode="w+", dtype="<u2", shape=(height, width)) if alpha_path else None
            try:
                done = 0
                for y in range(0, height, tile):
                    for x in range(0, width, tile):
                        ctx.check_active()
                        sl = (slice(y, y + tile), slice(x, x + tile))
                        alpha = effective_alpha(subject[sl], None if source_alpha is None else source_alpha[sl])
                        if params.mode in ("solid", "asset"):
                            backdrop = params.solid_rgb if params.mode == "solid" else render_asset_tile(
                                background_rgb, (height, width), (y, min(y + tile, height), x, min(x + tile, width)), geometry)
                            if params.shadow_opacity:
                                y0, y1 = max(0, y - shadow_halo), min(height, y + tile + shadow_halo)
                                x0, x1 = max(0, x - shadow_halo), min(width, x + tile + shadow_halo)
                                core = (slice(y - y0, min(y + tile, height) - y0), slice(x - x0, min(x + tile, width) - x0))
                                shade = contact_shadow(subject[y0:y1, x0:x1], shadow_radius, shadow_dx, shadow_dy,
                                                       params.shadow_opacity, core)
                                backdrop = np.power(np.power(np.asarray(backdrop, np.float32), np.float32(2.4)) *
                                                      (1 - shade[..., None]), np.float32(1 / 2.4), dtype=np.float32)
                            rgb_out[sl] = solid(rgb[sl], alpha, backdrop)
                        else:
                            rgb_out[sl] = transparent(rgb[sl], alpha)
                            alpha_out[sl] = alpha_u16(alpha)
                        done += 1
                        ctx.progress(done, total)
                rgb_out.flush()
                if alpha_out is not None:
                    alpha_out.flush()
                reservation.validate()
            finally:
                rgb_out._mmap.close()
                if alpha_out is not None:
                    alpha_out._mmap.close()
            pixel_ref = artifacts.stage(rgb_path, reservation, "working_rgb", "application/octet-stream")
            alpha_ref = artifacts.stage(alpha_path, reservation, "working_alpha", "application/octet-stream") if alpha_path else None
            written = rgb_path.stat().st_size + (alpha_path.stat().st_size if alpha_path else 0)
    output_image = WorkingImage(pixels=pixel_ref, canvas=image.canvas, color=image.color, dtype="float32",
                                alpha=alpha_ref, transform_chain=image.transform_chain)
    report = ComposeReport(image=params.image, subject=params.subject.artifact, mode=params.mode,
                           pixel_count=height * width, subject_confidence=params.subject.confidence,
                           source_alpha_combined=image.alpha is not None,
                           transparent_rgb_zeroed=params.mode == "transparent",
                           asset_scale=params.asset_scale if params.mode == "asset" else None,
                           contact_shadow=params.shadow_opacity > 0)
    image_ref = artifacts.stage_contract(output_image, "working_image")
    ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "background_report")
    ctx.progress(total, total)
    outputs = [pixel_ref, *([alpha_ref] if alpha_ref else []), image_ref, report_ref]
    return OperationResult(outputs=outputs, review_required=params.subject.confidence < .9,
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def background_operations(config: ArtifactConfig) -> list[Operation]:
    def composed(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_alpha", "working_image", "background_report"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("background.compose", "cpu", composed)]

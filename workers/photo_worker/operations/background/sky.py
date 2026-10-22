"""Replace confirmed sky while preserving explicit structures and foreground."""
from __future__ import annotations

from contextlib import ExitStack
import numpy as np

from photo_worker.algorithms.background import asset_geometry, render_asset_tile, replace_sky_tile, sky_gains
from photo_worker.contracts.background import SkyParams, SkyReport
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile = ctx.lease.execution_profile
    estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != "cpu" or type(ram) is not int or ram < 1_073_741_824:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(SkyParams)
        image = artifacts.contract(params.image, WorkingImage)
        sky_image = artifacts.contract(params.sky, WorkingImage)
        if (image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1"
                or sky_image.dtype != "float32" or sky_image.color != image.color or sky_image.alpha is not None
                or params.sky_mask.canvas != image.canvas or params.sky_mask.transform_chain != image.transform_chain
                or params.protected.canvas != image.canvas or params.protected.transform_chain != image.transform_chain
                or params.sky_mask.confidence < .75 or params.protected.confidence < .75
                or image.canvas.width * image.canvas.height > 200_000_000):
            raise ValueError("SKY_GEOMETRY_INVALID")
        geometry = asset_geometry(sky_image.canvas.width, sky_image.canvas.height,
                                  image.canvas.width, image.canvas.height,
                                  params.asset_scale, params.position_x, params.position_y)
        required = [image.pixels, params.sky_mask.artifact, params.protected.artifact, sky_image.pixels,
                    *([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):
            raise ValueError("SKY_INPUT_UNDECLARED")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc

    height, width, tile = image.canvas.height, image.canvas.width, params.tile
    tiles = ((height + tile - 1) // tile) * ((width + tile - 1) // tile)
    total = tiles * 2 + 2
    ctx.progress(0, total)
    with ExitStack() as stack:
        def mapped(ref, shape):
            local = stack.enter_context(artifacts.input(ref, suffix=".npy"))
            return stack.enter_context(read_array(local.path, shape, "float32", ctx.check_active, unit_interval=True))
        rgb = mapped(image.pixels, (height, width, 3))
        sky = mapped(params.sky_mask.artifact, (height, width))
        protected = mapped(params.protected.artifact, (height, width))
        asset = mapped(sky_image.pixels, (sky_image.canvas.height, sky_image.canvas.width, 3))
        source_sum = np.zeros(3, np.float64); asset_sum = np.zeros(3, np.float64); weight = 0.0; done = 0
        for y in range(0, height, tile):
            for x in range(0, width, tile):
                ctx.check_active(); y1, x1 = min(y + tile, height), min(x + tile, width)
                effective = np.clip(sky[y:y1, x:x1] * (1 - protected[y:y1, x:x1]), 0, 1).astype(np.float32)
                effective[protected[y:y1, x:x1] > 0] = 0
                rendered = render_asset_tile(asset, (height, width), (y, y1, x, x1), geometry)
                source_linear = np.power(rgb[y:y1, x:x1], np.float32(2.4), dtype=np.float32)
                asset_linear = np.power(rendered, np.float32(2.4), dtype=np.float32)
                source_sum += np.sum(source_linear * effective[..., None], axis=(0, 1), dtype=np.float64)
                asset_sum += np.sum(asset_linear * effective[..., None], axis=(0, 1), dtype=np.float64)
                weight += float(np.sum(effective, dtype=np.float64))
                done += 1; ctx.progress(done, total)
        if weight < 1:
            raise OperationFailure("MASK_EMPTY")
        gains = sky_gains(source_sum, asset_sum, weight, params.match_limit)
        changed = 0
        with artifacts.scratch.reserve(height * width * 12 + 8192) as reservation:
            path = reservation.path("sky-rgb", ".npy")
            target = np.lib.format.open_memmap(path, mode="w+", dtype="<f4", shape=(height, width, 3))
            try:
                for y in range(0, height, tile):
                    for x in range(0, width, tile):
                        ctx.check_active(); y1, x1 = min(y + tile, height), min(x + tile, width)
                        rendered = render_asset_tile(asset, (height, width), (y, y1, x, x1), geometry)
                        output, effective = replace_sky_tile(rgb[y:y1, x:x1], rendered, sky[y:y1, x:x1],
                                                             protected[y:y1, x:x1], gains)
                        target[y:y1, x:x1] = output
                        changed += int(np.count_nonzero(effective > 0))
                        done += 1; ctx.progress(done, total)
                target.flush(); reservation.validate()
            finally:
                target._mmap.close()
            pixel_ref = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream")
            written = path.stat().st_size
    output_image = WorkingImage(pixels=pixel_ref, canvas=image.canvas, color=image.color, dtype="float32",
                                alpha=image.alpha, transform_chain=image.transform_chain)
    report = SkyReport(image=params.image, sky_mask=params.sky_mask.artifact, protected=params.protected.artifact,
                       sky=params.sky, pixel_count=height * width, changed_pixels=changed,
                       match_limit=params.match_limit, gains_rgb=gains.tolist())
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "sky_report"); ctx.progress(total, total)
    return OperationResult(outputs=[pixel_ref, image_ref, report_ref],
                           review_required=min(params.sky_mask.confidence, params.protected.confidence) < .9,
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def sky_operations(config: ArtifactConfig) -> list[Operation]:
    def replaced(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_image", "sky_report"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("background.sky", "cpu", replaced)]

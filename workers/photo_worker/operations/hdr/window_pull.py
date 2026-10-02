"""Recover a confirmed window region from an aligned darker exposure."""
from __future__ import annotations

from contextlib import ExitStack
import numpy as np

from photo_worker.algorithms.hdr import window_pull
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.hdr import WindowParams, WindowReport
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
        params = artifacts.params(WindowParams)
        image = artifacts.contract(params.image, WorkingImage)
        dark = artifacts.contract(params.dark, WorkingImage)
        if (image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1"
                or dark.dtype != "float32" or dark.color != image.color
                or dark.canvas != image.canvas or dark.transform_chain != image.transform_chain
                or dark.alpha != image.alpha or params.mask.canvas != image.canvas
                or params.mask.transform_chain != image.transform_chain or params.mask.confidence < .9
                or image.canvas.width * image.canvas.height > 200_000_000):
            raise ValueError("WINDOW_GEOMETRY_INVALID")
        required = [image.pixels, dark.pixels, params.mask.artifact, *([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):
            raise ValueError("WINDOW_INPUT_UNDECLARED")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc

    height, width, tile = image.canvas.height, image.canvas.width, params.tile
    tiles = ((height + tile - 1) // tile) * ((width + tile - 1) // tile)
    total = tiles + 2; ctx.progress(0, total)
    changed = clipped = done = 0
    with ExitStack() as stack:
        def mapped(ref, shape):
            local = stack.enter_context(artifacts.input(ref, suffix=".npy"))
            return stack.enter_context(read_array(local.path, shape, "float32", ctx.check_active, unit_interval=True))
        rgb = mapped(image.pixels, (height, width, 3))
        dark_rgb = mapped(dark.pixels, (height, width, 3))
        mask = mapped(params.mask.artifact, (height, width))
        for y in range(0, height, tile):
            ctx.check_active(); changed += int(np.count_nonzero(mask[y:y + tile] > 0))
        if changed == 0:
            raise OperationFailure("MASK_EMPTY")
        with artifacts.scratch.reserve(height * width * 12 + 8192) as reservation:
            path = reservation.path("window-pull-rgb", ".npy")
            target = np.lib.format.open_memmap(path, mode="w+", dtype="<f4", shape=(height, width, 3))
            try:
                for y in range(0, height, tile):
                    for x in range(0, width, tile):
                        ctx.check_active(); sl = (slice(y, y + tile), slice(x, x + tile))
                        output, _, tile_clipped = window_pull(rgb[sl], dark_rgb[sl], mask[sl],
                                                              params.reference_exposure_seconds,
                                                              params.dark_exposure_seconds)
                        target[sl] = output; clipped += tile_clipped
                        done += 1; ctx.progress(done, total)
                target.flush(); reservation.validate()
            finally:
                target._mmap.close()
            pixel_ref = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream")
            written = path.stat().st_size
    fraction = clipped / changed
    output_image = WorkingImage(pixels=pixel_ref, canvas=image.canvas, color=image.color, dtype="float32",
                                alpha=image.alpha, transform_chain=image.transform_chain)
    report = WindowReport(image=params.image, dark=params.dark, mask=params.mask.artifact,
                          pixel_count=height * width, changed_pixels=changed,
                          exposure_ratio=params.reference_exposure_seconds / params.dark_exposure_seconds,
                          unrecoverable_fraction=fraction,
                          warnings=["all_exposures_clipped"] if clipped else [])
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "window_report"); ctx.progress(total, total)
    return OperationResult(outputs=[pixel_ref, image_ref, report_ref], review_required=clipped > 0,
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def hdr_operations(config: ArtifactConfig) -> list[Operation]:
    def window(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_image", "window_report"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("hdr.window_pull", "cpu", window)]

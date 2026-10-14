"""Compose a confirmed subject over transparency or a solid background."""
from __future__ import annotations

from contextlib import ExitStack

from .prepare import prepare_background
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
        with prepare_background(rgb, subject, source_alpha, background_rgb, params,
                artifacts.scratch, check_active=ctx.check_active,
                progress=lambda done:ctx.progress(done,total)) as prepared:
            rgb_path,alpha_path,reservation=prepared.rgb,prepared.alpha,prepared.reservation
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

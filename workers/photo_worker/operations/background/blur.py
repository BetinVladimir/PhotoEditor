"""Depth-layered artistic background blur with protected foreground."""
from __future__ import annotations

from contextlib import ExitStack

from .prepare_blur import prepare_blur
from photo_worker.contracts.background import BlurParams, BlurReport
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
        params = artifacts.params(BlurParams)
        image = artifacts.contract(params.image, WorkingImage)
        if (image.dtype != "float32" or image.color.profile_id != "work_rec2020_gamma24_v1"
                or params.subject.canvas != image.canvas or params.subject.transform_chain != image.transform_chain
                or params.depth.canvas != image.canvas or params.depth.transform_chain != image.transform_chain
                or params.subject.confidence < .75 or image.canvas.width * image.canvas.height > 200_000_000):
            raise ValueError("BLUR_GEOMETRY_INVALID")
        required = [image.pixels, params.subject.artifact, params.depth.artifact,
                    *([params.depth.invalid_mask] if params.depth.invalid_mask else []),
                    *([image.alpha] if image.alpha else [])]
        if any(ref not in ctx.lease.command.inputs for ref in required):
            raise ValueError("BLUR_INPUT_UNDECLARED")
        if params.depth.invalid_mask is not None:
            raise ValueError("BLUR_DEPTH_INCOMPLETE")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc

    height, width, tile = image.canvas.height, image.canvas.width, params.tile
    tiles = ((height + tile - 1) // tile) * ((width + tile - 1) // tile)
    total = tiles + 3
    ctx.progress(0, total)
    with ExitStack() as stack:
        def mapped(ref, shape):
            local = stack.enter_context(artifacts.input(ref, suffix=".npy"))
            return stack.enter_context(read_array(local.path, shape, "float32", ctx.check_active, unit_interval=True))
        rgb = mapped(image.pixels, (height, width, 3))
        subject = mapped(params.subject.artifact, (height, width))
        depth = mapped(params.depth.artifact, (height, width))
        with prepare_blur(rgb,subject,depth,artifacts.scratch,strength=params.strength,
                tile=tile,check_active=ctx.check_active,
                progress=lambda done:ctx.progress(done,total)) as prepared:
            path,reservation=prepared.rgb,prepared.reservation
            subject_depth,radii=prepared.subject_depth,prepared.radii
            pixel_ref = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream")
            written = path.stat().st_size
    output_image = WorkingImage(pixels=pixel_ref, canvas=image.canvas, color=image.color, dtype="float32",
                                alpha=image.alpha, transform_chain=image.transform_chain)
    report = BlurReport(image=params.image, subject=params.subject.artifact, depth=params.depth.artifact,
                        pixel_count=height * width, strength=params.strength, maximum_radius=max(radii),
                        subject_depth=subject_depth)
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "blur_report"); ctx.progress(total, total)
    return OperationResult(outputs=[pixel_ref, image_ref, report_ref], review_required=params.subject.confidence < .9,
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def blur_operations(config: ArtifactConfig) -> list[Operation]:
    def blurred(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_image", "blur_report"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("background.blur", "cpu", blurred)]

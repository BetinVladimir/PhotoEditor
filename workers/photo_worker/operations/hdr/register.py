"""Register a 3-9 frame HDR bracket and publish alignment/motion evidence."""
from __future__ import annotations

from contextlib import ExitStack
import numpy as np

from photo_worker.algorithms.hdr import motion_residual
from photo_worker.algorithms.registration import register_pair, register_mtb_translation
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.hdr import HDRRegisterParams, BracketTransform, BracketTransforms
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile = ctx.lease.execution_profile; estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != "cpu" or type(ram) is not int or ram < 536_870_912:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(HDRRegisterParams)
        images = [artifacts.contract(frame.image, WorkingImage) for frame in params.frames]
        reference = images[params.reference_index]
        for image in images:
            if (image.dtype != "float32" or image.color != reference.color or image.alpha is not None
                    or min(image.canvas.width, image.canvas.height) < 8
                    or image.canvas.width * image.canvas.height > 200_000_000):
                raise ValueError("HDR_BRACKET_INVALID")
            if image.pixels not in ctx.lease.command.inputs:
                raise ValueError("HDR_INPUT_UNDECLARED")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc
    total = len(images) * 2 + 1; ctx.progress(0, total); done = 0
    transforms = []; warnings = set(); review = False
    with ExitStack() as stack:
        ref_local = stack.enter_context(artifacts.input(reference.pixels, suffix=".npy"))
        ref_rgb = stack.enter_context(read_array(ref_local.path, (reference.canvas.height, reference.canvas.width, 3),
                                                 "float32", ctx.check_active, unit_interval=True))
        for index, (frame, image) in enumerate(zip(params.frames, images)):
            ctx.check_active()
            if index == params.reference_index:
                transforms.append(BracketTransform(frame_index=index, image=frame.image,
                    to_reference=np.eye(3).ravel().tolist(), overlap=1., median_reprojection=None,
                    inlier_count=0, motion_fraction=0., reference=True))
                done += 2; ctx.progress(done, total); continue
            with ExitStack() as source_stack:
                local = source_stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
                rgb = source_stack.enter_context(read_array(local.path, (image.canvas.height, image.canvas.width, 3),
                                                             "float32", ctx.check_active, unit_interval=True))
                try:
                    alignment = register_pair(rgb, ref_rgb, seed=(params.seed + index) & 0xffffffff,
                                              crop_aware=False, check_active=ctx.check_active)
                except ValueError as exc:
                    if str(exc) not in {"PAIR_FEATURES_INSUFFICIENT","PAIR_MATCHES_INSUFFICIENT","PAIR_ALIGNMENT_FAILED","PAIR_REPROJECTION_EXCEEDED"}:raise
                    alignment = register_mtb_translation(rgb, ref_rgb, check_active=ctx.check_active)
                done += 1; ctx.progress(done, total)
                motion = motion_residual(rgb, ref_rgb, alignment.before_to_after,
                                         frame.exposure_seconds, params.frames[params.reference_index].exposure_seconds)
            warnings.update(value for value in alignment.warnings if value != "background_mask_not_provided")
            if motion > .01: warnings.add("motion_detected"); review = True
            transforms.append(BracketTransform(frame_index=index, image=frame.image,
                to_reference=alignment.before_to_after.ravel().tolist(), overlap=alignment.overlap,
                median_reprojection=alignment.median_reprojection, inlier_count=alignment.inlier_count,
                motion_fraction=motion, reference=False))
            done += 1; ctx.progress(done, total)
    manifest = BracketTransforms(frames=params.frames, reference_index=params.reference_index,
                                 reference_canvas=reference.canvas, wb_recipe=params.wb_recipe,
                                 transforms=transforms, warnings=sorted(warnings))
    output = artifacts.stage_contract(manifest, "bracket_transforms"); ctx.progress(total, total)
    return OperationResult(outputs=[output], review_required=review or bool(warnings),
                           bytes_written=len(canonical_bytes(manifest)))


def register_operations(config: ArtifactConfig) -> list[Operation]:
    def registered(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"bracket_transforms"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("hdr.register", "cpu", registered)]

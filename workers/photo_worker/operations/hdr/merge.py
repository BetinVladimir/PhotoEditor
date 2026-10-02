"""Tile-bounded Debevec radiance merge or explicitly non-radiometric Mertens fusion."""
from __future__ import annotations

from contextlib import ExitStack
import cv2
import numpy as np

from photo_worker.algorithms.hdr import calibrate_debevec, merge_debevec_tile, reinhard_global_tile
from photo_worker.algorithms.registration import PairAlignment
from photo_worker.algorithms.warp import warp_pair_tiles
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.hdr import BracketTransforms, HDRMergeParams, HDRReport
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile = ctx.lease.execution_profile; estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != "cpu" or type(ram) is not int or ram < 1_073_741_824:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(HDRMergeParams)
        registration = artifacts.contract(params.registration, BracketTransforms)
        images = [artifacts.contract(frame.image, WorkingImage) for frame in registration.frames]
        reference = images[registration.reference_index]
        for image in images:
            if image.dtype != "float32" or image.color != reference.color or image.alpha is not None or image.pixels not in ctx.lease.command.inputs:
                raise ValueError("HDR_MERGE_SERIES_INVALID")
        if reference.canvas.width * reference.canvas.height > 200_000_000:
            raise ValueError("HDR_PIXEL_LIMIT")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc
    height, width = reference.canvas.height, reference.canvas.width
    tile_count = ((height + params.tile - 1) // params.tile) * ((width + params.tile - 1) // params.tile)
    total = tile_count * (2 if params.mode == "debevec" else 1) + 3; ctx.progress(0, total)
    exposures = [frame.exposure_seconds for frame in registration.frames]
    matrices = [np.asarray(item.to_reference, np.float64).reshape(3, 3) for item in registration.transforms]
    alignments = [PairAlignment(matrix, item.overlap, item.median_reprojection, item.inlier_count,
                                "user_confirmed" if item.reference else "measured", False, ())
                  for matrix, item in zip(matrices, registration.transforms)]
    with ExitStack() as stack:
        arrays = []
        for image in images:
            local = stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
            arrays.append(stack.enter_context(read_array(local.path, (image.canvas.height, image.canvas.width, 3),
                                                           "float32", ctx.check_active, unit_interval=True)))
        response = calibrate_debevec(arrays, matrices, (height, width), exposures) if params.mode == "debevec" else None
        ctx.progress(1, total); done = 1; motion_count = clipped_count = 0
        reservation_bytes = height * width * (24 if params.mode == "debevec" else 12) + 16384
        with artifacts.scratch.reserve(reservation_bytes) as reservation:
            output_path = reservation.path("hdr-rgb", ".npy")
            output = np.lib.format.open_memmap(output_path, mode="w+", dtype="<f4", shape=(height, width, 3))
            radiance_path = reservation.path("hdr-radiance", ".npy") if params.mode == "debevec" else None
            radiance = np.lib.format.open_memmap(radiance_path, mode="w+", dtype="<f4", shape=(height, width, 3)) if radiance_path else None
            try:
                generators = [iter(warp_pair_tiles(array, alignment, (height, width), tile=params.tile, check_active=ctx.check_active))
                              for array, alignment in zip(arrays, alignments)]
                log_sum = 0.; log_count = 0
                mertens = cv2.createMergeMertens(1., 1., 0.) if params.mode == "mertens" else None
                for _ in range(tile_count):
                    tiles = [next(generator) for generator in generators]
                    if len({(tile.x, tile.y, tile.rgb.shape) for tile in tiles}) != 1: raise ValueError("HDR_TILE_ORDER_INVALID")
                    current = tiles[registration.reference_index]; ys = slice(current.y, current.y + current.rgb.shape[0]); xs = slice(current.x, current.x + current.rgb.shape[1])
                    if params.mode == "debevec":
                        merged, motion, clipped = merge_debevec_tile([tile.rgb for tile in tiles], [tile.valid for tile in tiles], exposures, response, registration.reference_index)
                        radiance[ys, xs] = merged; motion_count += int(np.count_nonzero(motion)); clipped_count += int(np.count_nonzero(clipped))
                        luma = np.sum(merged * np.array([.2627, .678, .0593], np.float32), axis=2)
                        log_sum += float(np.sum(np.log(np.maximum(luma, 1e-8)), dtype=np.float64)); log_count += luma.size
                    else:
                        ref = tiles[registration.reference_index].rgb
                        ref_linear = np.power(ref, np.float32(2.4), dtype=np.float32)
                        ref_luma = np.sum(ref_linear * np.array([.2627, .678, .0593], np.float32), axis=2)
                        motion = np.zeros(ref.shape[:2], bool); clipped = np.ones(ref.shape[:2], bool); cleaned = []
                        for tile, exposure in zip(tiles, exposures):
                            normalized = np.power(tile.rgb, np.float32(2.4), dtype=np.float32) * np.float32(exposures[registration.reference_index] / exposure)
                            delta = np.abs(np.sum(normalized * np.array([.2627, .678, .0593], np.float32), axis=2) - ref_luma)
                            moving = (delta > .05) & tile.valid; motion |= moving
                            z = np.round(np.clip(tile.rgb, 0, 1) * 255).astype(np.uint8)
                            clipped &= ((z <= 1) | (z >= 254)).all(axis=2) | ~tile.valid
                            cleaned.append(np.where((tile.valid & ~moving)[..., None], tile.rgb, ref).astype(np.float32))
                        fused = mertens.process(cleaned)
                        output[ys, xs] = np.clip(fused, 0, 1).astype(np.float32)
                        motion_count += int(np.count_nonzero(motion)); clipped_count += int(np.count_nonzero(clipped))
                    done += 1; ctx.progress(done, total)
                if radiance is not None:
                    radiance.flush(); log_average = float(np.exp(log_sum / log_count))
                    for y in range(0, height, params.tile):
                        for x in range(0, width, params.tile):
                            ctx.check_active(); sl = (slice(y, y + params.tile), slice(x, x + params.tile))
                            output[sl] = reinhard_global_tile(radiance[sl], log_average)
                            done += 1; ctx.progress(done, total)
                output.flush(); reservation.validate()
            except ValueError as exc:
                raise OperationFailure("INPUT_INVALID") from exc
            finally:
                output._mmap.close()
                if radiance is not None: radiance._mmap.close()
            pixel_ref = artifacts.stage(output_path, reservation, "working_rgb", "application/octet-stream")
            written = output_path.stat().st_size
    pixels = height * width; warnings = []
    if motion_count: warnings.append("motion_deghosted")
    if clipped_count: warnings.append("all_exposures_clipped")
    if params.mode == "mertens": warnings.append("non_radiometric_mertens")
    if registration.warnings: warnings.append("registration_review")
    output_image = WorkingImage(pixels=pixel_ref, canvas=reference.canvas, color=reference.color,
                                dtype="float32", alpha=None, transform_chain=reference.transform_chain)
    report = HDRReport(registration=params.registration, mode=params.mode, pixel_count=pixels,
                       response_calibrated=params.mode == "debevec", radiometric=params.mode == "debevec",
                       deghost_fraction=motion_count / pixels, unrecoverable_fraction=clipped_count / pixels,
                       tonemap="reinhard_intensity0_light1_color0_gamma1" if params.mode == "debevec" else "mertens_default_v1",
                       warnings=warnings)
    image_ref = artifacts.stage_contract(output_image, "working_image"); ctx.progress(total - 1, total)
    report_ref = artifacts.stage_contract(report, "hdr_report"); ctx.progress(total, total)
    return OperationResult(outputs=[pixel_ref, image_ref, report_ref], review_required=bool(warnings),
                           bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def merge_operations(config: ArtifactConfig) -> list[Operation]:
    def merged(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_image", "hdr_report"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("hdr.merge", "cpu", merged)]

"""Apply an authorized sensor-space dust selection to one aligned frame."""
from __future__ import annotations

from contextlib import ExitStack
import cv2
import numpy as np

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.dust import (DustCandidates, DustRepairEvidence, DustRepairParams,
                                         DustRepairReport, DustReview, accepted_dust_candidates)
from photo_worker.contracts.images import WorkingImage
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def execute_repair(ctx: ExecutionContext, artifacts: TaskArtifacts) -> OperationResult:
    profile = ctx.lease.execution_profile; estimate = profile.get("resource_estimate")
    ram = estimate.get("ram_bytes") if isinstance(estimate, dict) else None
    if profile.get("backend") != "cpu" or type(ram) is not int or ram < 536_870_912:
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(DustRepairParams)
        image = artifacts.contract(params.image, WorkingImage)
        proposal = artifacts.contract(params.proposal, DustCandidates) if params.proposal is not None else None
        review = params.review
        selected = accepted_dust_candidates(params, proposal, review)
        if image.dtype != "float32" or image.alpha is not None or image.canvas.orientation != 1:
            raise ValueError("Invalid dust repair image")
        if image.pixels not in ctx.lease.command.inputs:
            raise ValueError("Undeclared dust repair pixels")
    except ValueError as exc:
        code = str(exc) if str(exc) in ("STALE_DUST_REVIEW", "UNKNOWN_DUST_CANDIDATE") else "INPUT_INVALID"
        raise OperationFailure(code) from exc

    h, w = image.canvas.height, image.canvas.width
    crop = proposal.raw_crop if proposal is not None else None
    ctx.progress(0, 3); outputs = []; written = 0; evidence = []
    if selected:
        with ExitStack() as stack:
            local = stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
            rgb = stack.enter_context(read_array(local.path, (h, w, 3), "float32", ctx.check_active, unit_interval=True))
            mask = np.zeros((h, w), np.uint8)
            mapped = []
            for candidate in selected:
                x = (candidate.sensor_x - crop.x + .5) * w / crop.width - .5
                y = (candidate.sensor_y - crop.y + .5) * h / crop.height - .5
                radius = max(1., candidate.radius_sensor_px * .5 * (w / crop.width + h / crop.height))
                if x + radius < 0 or y + radius < 0 or x - radius >= w or y - radius >= h:
                    raise OperationFailure("INPUT_INVALID")
                before = int(np.count_nonzero(mask))
                cv2.circle(mask, (round(x), round(y)), max(1, round(radius)), 255, -1, lineType=cv2.LINE_8)
                mapped.append((candidate, x, y, radius, before))
            ctx.check_active()
            repaired = np.empty_like(rgb)
            inpaint_radius = min(32., max(item[3] for item in mapped) + 2.)
            for channel in range(3):
                repaired[:, :, channel] = cv2.inpaint(rgb[:, :, channel], mask, inpaint_radius, cv2.INPAINT_TELEA)
            np.clip(repaired, 0, 1, out=repaired)
            with artifacts.scratch.reserve(h * w * 12 + 8192) as reservation:
                path = reservation.path("dust-repaired-rgb", ".npy")
                with artifacts.scratch.open_writer(path, reservation) as output:
                    np.save(output, repaired, allow_pickle=False)
                pixels = artifacts.stage(path, reservation, "working_rgb", "application/octet-stream")
                written = path.stat().st_size; outputs.append(pixels)
            output_image = image.model_copy(update={"pixels": pixels})
            for candidate, x, y, radius, _ in mapped:
                yy, xx = np.ogrid[:h, :w]
                applied = int(np.count_nonzero((xx - round(x)) ** 2 + (yy - round(y)) ** 2 <= round(radius) ** 2))
                evidence.append(DustRepairEvidence(candidate_id=candidate.id, center_x=x, center_y=y,
                                                   radius=radius, applied_pixels=applied))
    else:
        output_image = image
    ctx.progress(1, 3)
    image_ref = artifacts.stage_contract(output_image, "working_image"); outputs.append(image_ref)
    ctx.progress(2, 3)
    report = DustRepairReport(image=params.image, proposal=params.proposal,
                              review_revision=review.revision if review is not None else 1, candidates=evidence,
                              warnings=[] if selected else ["no_accepted_candidates"])
    report_ref = artifacts.stage_contract(report, "dust_repair_report"); outputs.append(report_ref)
    ctx.progress(3, 3)
    return OperationResult(outputs=outputs, bytes_written=written + len(canonical_bytes(output_image)) + len(canonical_bytes(report)))


def dust_repair_operations(config: ArtifactConfig) -> list[Operation]:
    def repair(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"working_rgb", "working_image", "dust_repair_report"})) as artifacts:
            return execute_repair(ctx, artifacts)
    return [Operation("sensor.dust_repair", "cpu", repair)]

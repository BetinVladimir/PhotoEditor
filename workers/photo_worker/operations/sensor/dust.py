"""Detect recurrent dust in a sensor-aligned image series."""
from __future__ import annotations

from contextlib import ExitStack

from photo_worker.algorithms.dust import detect_frame_dust, map_to_sensor, repeated_dust
from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.contracts.dust import DustCandidate, DustCandidates, DustParams
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
        params = artifacts.params(DustParams)
        images = [artifacts.contract(ref, WorkingImage) for ref in params.frames]
        first = images[0]
        for image in images:
            if (image.dtype != "float32" or image.color != first.color or image.alpha is not None
                    or image.canvas.width != first.canvas.width or image.canvas.height != first.canvas.height
                    or image.canvas.orientation != 1 or image.pixels not in ctx.lease.command.inputs):
                raise ValueError("DUST_SERIES_INVALID")
            if image.canvas.width * image.canvas.height > 200_000_000:
                raise ValueError("DUST_PIXEL_LIMIT")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc
    total = len(images) + 1; ctx.progress(0, total); detected = []
    crop = (params.raw_crop.x, params.raw_crop.y, params.raw_crop.width, params.raw_crop.height)
    for index, image in enumerate(images):
        with ExitStack() as stack:
            local = stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
            rgb = stack.enter_context(read_array(local.path, (image.canvas.height, image.canvas.width, 3),
                                                  "float32", ctx.check_active, unit_interval=True))
            frame = [map_to_sensor(item, (image.canvas.height, image.canvas.width), crop)
                     for item in detect_frame_dust(rgb, ctx.check_active)]
        detected.append(frame); ctx.progress(index + 1, total)
    repeated = repeated_dust(detected, params.recurrence)
    candidates = []
    for item, observed, eligible in repeated:
        radius = min(item.radius, 32)
        identity = {"algorithm": "sensor_dust_log_v1", "camera_id": params.camera_id,
                    "sensor": [params.sensor_width, params.sensor_height], "x": item.x, "y": item.y,
                    "radius": radius, "frames": [ref.sha256 for ref in params.frames]}
        candidates.append(DustCandidate(id=canonical_hash(identity), sensor_x=item.x, sensor_y=item.y,
                                        radius_sensor_px=radius, observed_frames=observed,
                                        eligible_frames=eligible, recurrence=observed / eligible))
    manifest = DustCandidates(frames=params.frames, camera_id=params.camera_id,
                              sensor_width=params.sensor_width, sensor_height=params.sensor_height,
                              raw_crop=params.raw_crop, candidates=candidates,
                              warnings=[] if candidates else ["no_repeated_dust"])
    output = artifacts.stage_contract(manifest, "dust_candidates"); ctx.progress(total, total)
    return OperationResult(outputs=[output], review_required=bool(candidates), bytes_written=len(canonical_bytes(manifest)))


def sensor_operations(config: ArtifactConfig) -> list[Operation]:
    def dust(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"dust_candidates"})) as artifacts:
            return execute(ctx, artifacts)
    return [Operation("sensor.dust", "cpu", dust)]

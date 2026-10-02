"""Render an editor recipe directly to a color-managed immutable preview."""
from contextlib import ExitStack

import numpy as np
from PIL import Image

from photo_worker.algorithms.color import ColorDiagnostics, apply_global, apply_strength
from photo_worker.color.icc import ICCRegistry, LittleCMS
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.editor import EditorPreviewParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.contracts.media import DerivativeManifest
from photo_worker.operations.export.encode import _resize
from photo_worker.operations.media.derivative import QUALITY, _write, dimensions, media_profile_hash
from photo_worker.runtime.arrays import read_array
from photo_worker.runtime.artifacts import ArtifactConfig, TaskArtifacts
from photo_worker.runtime.executor import ExecutionContext, Operation, OperationFailure, OperationResult


def execute(ctx: ExecutionContext, artifacts: TaskArtifacts, registry: ICCRegistry) -> OperationResult:
    if ctx.lease.execution_profile.get("backend") != "cpu":
        raise OperationFailure("CONFIG_INVALID")
    try:
        params = artifacts.params(EditorPreviewParams)
        image = artifacts.contract(params.image, WorkingImage)
        style = artifacts.style(params.style) if params.style is not None else None
        source_profile = registry.resolve(image.color.profile_id)
        target_profile = registry.resolve("srgb_v1")
        if source_profile.sha256 != image.color.icc_sha256 or image.dtype != "float32":
            raise ValueError("PREVIEW_SOURCE_INVALID")
        if style is not None and style.mode != "fixed":
            raise ValueError("ADAPTIVE_PREVIEW_REQUIRES_STATS")
        unsupported = (params.modules.local, params.modules.beauty, params.modules.defects,
                       params.modules.geometry, params.modules.extensions)
        if any(value is not None for value in unsupported) or (style is not None and style.local.enabled):
            raise ValueError("PREVIEW_MODULE_REQUIRES_PIPELINE")
        x, y, width, height = params.roi or (0, 0, image.canvas.width, image.canvas.height)
        if x + width > image.canvas.width or y + height > image.canvas.height:
            raise ValueError("PREVIEW_ROI_OUTSIDE_CANVAS")
    except ValueError as exc:
        raise OperationFailure("INPUT_INVALID") from exc
    ctx.progress(0, 4)
    with ExitStack() as stack:
        local = stack.enter_context(artifacts.input(image.pixels, suffix=".npy"))
        source = stack.enter_context(read_array(local.path,
            (image.canvas.height, image.canvas.width, 3), "float32",
            ctx.check_active, unit_interval=True))[y:y + height, x:x + width]
        global_color = params.modules.global_ or (style.global_ if style is not None else None)
        changed = apply_global(source, global_color, ColorDiagnostics()) if global_color else source
        strength = params.modules.strength if params.modules.strength is not None else (style.strength if style else 1.0)
        rendered = apply_strength(source, changed, strength)
        variant = "preview1600" if params.scale == "proxy" else "preview2560"
        out_width, out_height = dimensions(width, height, variant)
        resized = _resize(rendered, out_width, out_height)
        converted = LittleCMS().convert(np.round(resized * 65535).astype(np.uint16),
            source_profile.data, target_profile.data).astype(np.float32) / 65535
        ctx.progress(1, 4)
        with artifacts.scratch.reserve(out_width * out_height * 5 + 2 * 1024 * 1024) as reservation:
            path = reservation.planned_path("editor-preview", ".jpg")
            _write(path, converted, None, target_profile.data, QUALITY[variant])
            reservation.validate_file(path)
            with Image.open(path) as check:
                check.verify()
            derivative = artifacts.stage(path, reservation, "derivative", "image/jpeg")
            written = path.stat().st_size
    profile_hash = media_profile_hash(variant, target_profile.sha256)
    manifest = DerivativeManifest(source=params.image, derivative=derivative, variant=variant,
        profile_hash=profile_hash, width=out_width, height=out_height, media_type="image/jpeg",
        color_profile="srgb", metadata_policy="stripped", alpha=False)
    manifest_ref = artifacts.stage_contract(manifest, "derivative_manifest")
    ctx.progress(4, 4)
    return OperationResult(outputs=[derivative, manifest_ref],
        bytes_written=written + len(canonical_bytes(manifest)))


def preview_operations(config: ArtifactConfig, icc_root=None) -> list[Operation]:
    if icc_root is None:
        return []
    registry = ICCRegistry(icc_root)

    def run(ctx):
        with TaskArtifacts.create(ctx, config, frozenset({"derivative", "derivative_manifest"})) as artifacts:
            return execute(ctx, artifacts, registry)

    return [Operation("editor.preview", "media", run)]

"""Attempt-scoped staging of a completely rendered region derivative."""
from collections.abc import Callable
from hashlib import sha256

import numpy as np

from photo_worker.contracts.base import canonical_bytes, canonical_hash
from photo_worker.runtime.executor import OperationResult
from .contracts import RegionPreviewParams
from .presentation import RegionPresentation, region_cache_key, write_region_presentation
from .result import build_region_manifest, verify_region_encoded


def stage_region_result(artifacts, params: RegionPreviewParams, source_body: bytes,
                        presentation: RegionPresentation, *,
                        check_active: Callable[[], None] = lambda: None) -> OperationResult:
    """Stage through existing attempt upload fences; does not admit/adopt a job."""
    check_active()
    image = params.validate_source(source_body)
    alpha = image.alpha is not None if params.execution is None else params.execution.stages[-1].target_has_alpha
    geometry=params.geometry if presentation.projection is None else presentation.projection.geometry
    width, height = geometry.output_width, geometry.output_height
    if (not isinstance(presentation.rgb, np.ndarray)
            or presentation.rgb.shape != (height, width, 3)
            or presentation.rgb.dtype != np.float32
            or (presentation.alpha is not None) != alpha
            or sha256(presentation.profile).hexdigest() != presentation.profile_sha256
            or presentation.cache_key != region_cache_key(params, presentation.profile_sha256, alpha,projection=presentation.projection)):
        raise ValueError('REGION_PRESENTATION_INVALID')
    values = [presentation.rgb]
    if alpha:
        if (not isinstance(presentation.alpha, np.ndarray)
                or presentation.alpha.shape != (height, width)
                or presentation.alpha.dtype != np.float32):
            raise ValueError('REGION_PRESENTATION_INVALID')
        values.append(presentation.alpha)
    for value in values:
        if not np.isfinite(value).all() or np.any((value < 0) | (value > 1)):
            raise ValueError('REGION_PRESENTATION_INVALID')
    check_active()
    with artifacts.scratch.reserve(width * height * 5 + 2 * 1024 * 1024) as reservation:
        path = reservation.planned_path('region-preview', '.webp' if alpha else '.jpg')
        size = write_region_presentation(path, presentation, check_active=check_active)
        reservation.validate_file(path)
        if size > 16_777_216:
            raise ValueError('REGION_OUTPUT_TOO_LARGE')
        check_active()
        derivative = artifacts.stage(path, reservation, 'derivative', presentation.media_type)
        manifest = build_region_manifest(params, presentation, derivative, size)
        verify_region_encoded(manifest, params, path.read_bytes(),
            target_sha256=presentation.profile_sha256, expected_alpha=alpha)
    check_active()
    ref = artifacts.stage_contract(manifest, 'document')
    if ref.artifact_id.int == 0 or ref.sha256 != canonical_hash(manifest):
        raise ValueError('REGION_MANIFEST_UPLOAD_INVALID')
    check_active()
    return OperationResult(outputs=[derivative, ref],
        bytes_written=size + len(canonical_bytes(manifest)))

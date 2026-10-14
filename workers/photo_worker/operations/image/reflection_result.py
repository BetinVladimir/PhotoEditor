"""V28 reflection output closure; caller must verify pixels before publication."""

from dataclasses import dataclass

import numpy as np

from photo_worker.contracts.base import ArtifactRef, canonical_bytes, canonical_hash
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.operations.optical.contracts import ImageReport, ImageResult


@dataclass(frozen=True)
class ReflectionFiles:
    rgb: object
    changed: object
    generated: object
    reservation: object
    clipping_fraction: float


def stage_reflection_result(artifacts, params, source: WorkingImage,
                            files: ReflectionFiles) -> tuple[ArtifactRef, int]:
    """Stage a review-only ImageResult with two independently declared masks."""
    if (params.base.canvas_hash != canonical_hash(source.canvas)
            or not np.isfinite(files.clipping_fraction)
            or not 0 <= files.clipping_fraction <= 1):
        raise ValueError('REFLECTION_RESULT_INVALID')
    files.reservation.validate()
    try:
        paths = (files.rgb.resolve(strict=True),
                 files.changed.resolve(strict=True),
                 files.generated.resolve(strict=True))
        identities = tuple((path.stat().st_dev, path.stat().st_ino)
                           for path in paths)
    except (AttributeError, OSError, RuntimeError) as error:
        raise ValueError('REFLECTION_RESULT_INVALID') from error
    if len(set(paths)) != 3 or len(set(identities)) != 3:
        raise ValueError('REFLECTION_RESULT_INVALID')
    written = 0

    def stage(path, role):
        nonlocal written
        ref = artifacts.stage(path, files.reservation, role, 'application/x-npy')
        written += path.stat().st_size
        return ref

    def document(value, role):
        nonlocal written
        ref = artifacts.stage_contract(value, role)
        written += len(canonical_bytes(value))
        return ref

    pixels_ref = stage(files.rgb, 'pixels')
    image = source.model_copy(update={'pixels': pixels_ref})
    image_ref = document(image, 'working_image')
    changed_pixels = stage(files.changed, 'mask_pixels')
    generated_pixels = stage(files.generated, 'mask_pixels')
    changed = MaskRef(artifact=changed_pixels, canvas=source.canvas,
                      transform_chain=source.transform_chain, object_id=None,
                      selector='changed', confidence=1., model_lock_id=None,
                      protected=False)
    generated = MaskRef(artifact=generated_pixels, canvas=source.canvas,
                        transform_chain=source.transform_chain, object_id=None,
                        selector='generated', confidence=1.,
                        model_lock_id=params.model_lock_sha, protected=False)
    changed_ref = document(changed, 'mask')
    generated_ref = document(generated, 'mask')
    report = ImageReport(schema_version=1, status='review',
                         operation='image.reflection', skipped_count=0,
                         reason='reflection_model_proposal', outside_mask_max_error=0.,
                         clipping_fraction=files.clipping_fraction, identity=None,
                         transform=None, sources=[params.base], registrations=[])
    report_ref = document(report, 'report')
    result = ImageResult(schema_version=1, working_image=image_ref,
                         report=report_ref, changed_mask=changed_ref,
                         generated_mask=generated_ref)
    return document(result, 'document'), written

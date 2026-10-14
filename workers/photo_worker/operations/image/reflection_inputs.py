"""V28 reflection source closure before model or image IO."""

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_restoration import ImageReflectionParams
from photo_worker.operations.session.contracts import Calibration


def verify_reflection_inputs(params, source, target, protected, calibration,
                             declared_inputs, installed_lock_sha):
    """Reject document substitution, undeclared bytes, and mask role confusion."""
    if (not isinstance(params, ImageReflectionParams)
            or not isinstance(source, WorkingImage)
            or not isinstance(target, MaskRef)
            or not isinstance(protected, MaskRef)
            or not isinstance(calibration, Calibration)):
        raise ValueError('REFLECTION_INPUT_INVALID')
    if (params.model_lock_sha != installed_lock_sha
            or calibration.model_lock_sha != installed_lock_sha
            or not calibration.qualified):
        raise ValueError('REFLECTION_MODEL_UNAVAILABLE')
    for document, ref in ((source, params.base.source), (target, params.mask),
                          (protected, params.protected),
                          (calibration, params.calibration)):
        if canonical_hash(document) != ref.sha256:
            raise ValueError('REFLECTION_DOCUMENT_MISMATCH')
    if (source.dtype != 'float32' or source.color.profile_id not in
            ('work_rec2020_gamma24_v1', 'linear_srgb_v1')
            or canonical_hash(source.canvas) != params.base.canvas_hash
            or source.canvas.width * source.canvas.height > 16_000_000):
        raise ValueError('REFLECTION_SOURCE_INVALID')
    if (target.protected or not protected.protected
            or target.canvas != source.canvas or protected.canvas != source.canvas
            or target.transform_chain != source.transform_chain
            or protected.transform_chain != source.transform_chain):
        raise ValueError('REFLECTION_MASK_INVALID')
    required = [params.base.source, params.mask, params.protected,
                params.calibration, source.pixels, target.artifact,
                protected.artifact]
    if source.alpha is not None:
        required.append(source.alpha)
    for step in source.transform_chain.steps:
        if step.kind == 'dense_warp':
            required.append(step.inverse_field)
    if any(ref not in declared_inputs for ref in required):
        raise ValueError('REFLECTION_INPUT_UNDECLARED')

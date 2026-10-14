"""Relight document closure; current SQL/model authority remains caller-owned."""
from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.images import MaskRef, RelativeDepth, WorkingImage
from photo_worker.contracts.relight import ImageRelightParams
from photo_worker.operations.session.contracts import Calibration


def verify_relight_inputs(params, source, depth, subject, protected, calibration,
                          declared_inputs):
    """Refuse substituted documents and undeclared binary dependencies before IO.

    A qualified flag is necessary but does not establish trusted qualification.
    The execution adapter must independently recheck current runtime authority.
    """
    if not isinstance(params, ImageRelightParams) or not isinstance(source, WorkingImage):
        raise ValueError('RELIGHT_PARAMS_INVALID')
    if not isinstance(depth, RelativeDepth) or not all(
            isinstance(mask, MaskRef) for mask in (subject, protected)):
        raise ValueError('RELIGHT_INPUT_INVALID')
    if not isinstance(calibration, Calibration) or not calibration.qualified:
        raise ValueError('RELIGHT_CALIBRATION_INVALID')
    for document, ref in ((source, params.base.source), (depth, params.depth),
                          (subject, params.subject), (protected, params.protected),
                          (calibration, params.calibration)):
        if canonical_hash(document) != ref.sha256:
            raise ValueError('RELIGHT_DOCUMENT_MISMATCH')
    if source.dtype != 'float32' or canonical_hash(source.canvas) != params.base.canvas_hash:
        raise ValueError('RELIGHT_SOURCE_MISMATCH')
    if source.canvas.width * source.canvas.height > 200000000:
        raise ValueError('RELIGHT_PIXEL_LIMIT')
    if subject.protected or not protected.protected:
        raise ValueError('RELIGHT_MASK_ROLE_MISMATCH')
    for document in (depth, subject, protected):
        if document.canvas != source.canvas or document.transform_chain != source.transform_chain:
            raise ValueError('RELIGHT_GEOMETRY_MISMATCH')
    required = [params.base.source, params.depth, params.subject, params.protected,
                params.calibration, source.pixels, depth.artifact,
                subject.artifact, protected.artifact]
    if source.alpha is not None:
        required.append(source.alpha)
    if depth.invalid_mask is not None:
        required.append(depth.invalid_mask)
    # Dense transforms carry binary dependencies even when no resampling occurs.
    for step in source.transform_chain.steps:
        if step.kind == 'dense_warp':
            required.append(step.inverse_field)
    if any(ref not in declared_inputs for ref in required):
        raise ValueError('RELIGHT_INPUT_UNDECLARED')

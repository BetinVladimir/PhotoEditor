"""V28 donorless glare source/consent/model document closure."""

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.session.contracts import Calibration, FaceObservations


def verify_generative_glare_inputs(params, source, target, protected,
                                    confirmation, source_faces, calibration,
                                    declared_inputs, installed_lock_sha):
    """Reject donor substitution and undeclared pixel/transform dependencies."""
    if (not isinstance(params, PortraitGlareParams)
            or params.mode != 'generative' or params.donor is not None
            or params.donor_faces is not None
            or not isinstance(source, WorkingImage)
            or not isinstance(target, MaskRef)
            or not isinstance(protected, MaskRef)
            or not isinstance(confirmation, DonorConfirmation)
            or not isinstance(source_faces, FaceObservations)
            or not isinstance(calibration, Calibration)):
        raise ValueError('GLARE_GENERATIVE_INPUT_INVALID')
    if (not calibration.qualified
            or calibration.model_lock_sha != installed_lock_sha
            or params.model_lock_sha != installed_lock_sha
            or confirmation.source != params.base
            or confirmation.donor is not None
            or confirmation.donor_face_id is not None
            or confirmation.donor_observation_id is not None):
        raise ValueError('GLARE_GENERATIVE_MODEL_UNAVAILABLE')
    documents = ((source, params.base.source),
                 (target, params.glasses_mask),
                 (protected, params.protected),
                 (confirmation, params.confirmation),
                 (source_faces, params.source_faces),
                 (calibration, params.calibration))
    if any(canonical_hash(value) != ref.sha256 for value, ref in documents):
        raise ValueError('GLARE_GENERATIVE_DOCUMENT_MISMATCH')
    if (source.dtype != 'float32'
            or canonical_hash(source.canvas) != params.base.canvas_hash
            or source.canvas.width * source.canvas.height > 16_000_000
            or source_faces.binding != params.base):
        raise ValueError('GLARE_GENERATIVE_SOURCE_INVALID')
    if (target.protected or not protected.protected
            or any(mask.canvas != source.canvas
                   or mask.transform_chain != source.transform_chain
                   for mask in (target, protected))):
        raise ValueError('GLARE_GENERATIVE_MASK_INVALID')
    required = [ref for _, ref in documents]
    required += [source.pixels, target.artifact, protected.artifact]
    if source.alpha is not None:
        required.append(source.alpha)
    for step in source.transform_chain.steps:
        if step.kind == 'dense_warp':
            required.append(step.inverse_field)
    if any(ref not in declared_inputs for ref in required):
        raise ValueError('GLARE_GENERATIVE_INPUT_UNDECLARED')

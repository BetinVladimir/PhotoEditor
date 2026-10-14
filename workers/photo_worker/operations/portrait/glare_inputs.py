"""V28 donor glare document and binary closure before physical image IO."""

from photo_worker.contracts.base import canonical_hash
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.images import MaskRef, WorkingImage
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.operations.session.contracts import Calibration, FaceObservations


def verify_glare_donor_inputs(params, source, donor, target, protected,
                              confirmation, source_faces, donor_faces,
                              calibration, declared_inputs):
    """Require exact source/donor evidence closure; current consent is separate."""
    if (not isinstance(params, PortraitGlareParams) or params.mode != 'donor'
            or params.donor is None or params.donor_faces is None
            or not all(isinstance(image, WorkingImage) for image in (source, donor))
            or not all(isinstance(mask, MaskRef) for mask in (target, protected))
            or not isinstance(confirmation, DonorConfirmation)
            or not all(isinstance(faces, FaceObservations)
                       for faces in (source_faces, donor_faces))
            or not isinstance(calibration, Calibration)):
        raise ValueError('GLARE_DONOR_INPUT_INVALID')
    if (not calibration.qualified or calibration.model_lock_sha != params.model_lock_sha
            or confirmation.source != params.base or confirmation.donor != params.donor
            or confirmation.donor_face_id is None
            or confirmation.donor_observation_id is None):
        raise ValueError('GLARE_DONOR_EVIDENCE_INVALID')
    documents = ((source, params.base.source), (donor, params.donor.source),
                 (target, params.glasses_mask), (protected, params.protected),
                 (confirmation, params.confirmation),
                 (source_faces, params.source_faces),
                 (donor_faces, params.donor_faces),
                 (calibration, params.calibration))
    if any(canonical_hash(value) != ref.sha256 for value, ref in documents):
        raise ValueError('GLARE_DONOR_DOCUMENT_MISMATCH')
    if (source.dtype != 'float32' or donor.dtype != 'float32'
            or source.color != donor.color
            or canonical_hash(source.canvas) != params.base.canvas_hash
            or canonical_hash(donor.canvas) != params.donor.canvas_hash
            or source.canvas.width * source.canvas.height > 200_000_000
            or donor.canvas.width * donor.canvas.height > 200_000_000
            or source_faces.binding != params.base
            or donor_faces.binding != params.donor):
        raise ValueError('GLARE_DONOR_SOURCE_INVALID')
    if (target.protected or not protected.protected
            or any(mask.canvas != source.canvas
                   or mask.transform_chain != source.transform_chain
                   for mask in (target, protected))):
        raise ValueError('GLARE_DONOR_MASK_INVALID')
    required = [ref for _, ref in documents]
    required += [source.pixels, donor.pixels, target.artifact,
                 protected.artifact]
    for image in (source, donor):
        if image.alpha is not None:
            required.append(image.alpha)
        for step in image.transform_chain.steps:
            if step.kind == 'dense_warp':
                required.append(step.inverse_field)
    if any(ref not in declared_inputs for ref in required):
        raise ValueError('GLARE_DONOR_INPUT_UNDECLARED')

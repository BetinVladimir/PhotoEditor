"""V28 confirmed-donor glare repair using the established face patch geometry."""

import hashlib

from photo_worker.algorithms.portrait_donor_patch import prepare_donor_patch
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.donor_confirmation import DonorConfirmation
from photo_worker.contracts.portrait_open_eyes import PortraitOpenEyesParams
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.v2.portrait_donor_evidence import verify_open_eyes_faces


def verify_glare_face_geometry(params, confirmation_raw, source_faces_raw,
                               donor_faces_raw, source_size, donor_size,
                               min_face_edge_px):
    """Bind donor proof and observations before a patch can be planned."""
    if (not isinstance(params, PortraitGlareParams) or params.mode != 'donor'
            or params.donor is None or params.donor_faces is None):
        raise ValueError('GLARE_DONOR_INPUT_INVALID')
    owned = PortraitGlareParams.model_validate_json(canonical_bytes(params))
    raw = bytes(confirmation_raw)
    parse_json(raw, max_bytes=65_536)
    proof = DonorConfirmation.model_validate_json(raw)
    if (canonical_bytes(proof) != raw
            or hashlib.sha256(raw).hexdigest() != owned.confirmation.sha256
            or proof.source != owned.base or proof.donor != owned.donor
            or proof.donor_face_id is None or proof.donor_observation_id is None):
        raise ValueError('GLARE_DONOR_CONFIRMATION_INVALID')
    proxy = PortraitOpenEyesParams(schema_version=1, base=owned.base,
        strength=owned.strength, donor=owned.donor,
        source_face_id=proof.source_face_id, donor_face_id=proof.donor_face_id,
        eyes_mask=owned.glasses_mask, protected=owned.protected,
        confirmation=owned.confirmation, source_faces=owned.source_faces,
        donor_faces=owned.donor_faces)
    return verify_open_eyes_faces(proxy, raw, bytes(source_faces_raw),
                                 bytes(donor_faces_raw), source_size,
                                 donor_size, min_face_edge_px)


def prepare_glare_donor_patch(params, confirmation_raw, source_faces_raw,
                              donor_faces_raw, source_patch, donor_rgb,
                              glasses_mask, protected, source_size, donor_size,
                              source_origin, min_face_edge_px, check_active,
                              authority, *, encoding='linear', donor_alpha=None,
                              source_alpha=None):
    """Make a reviewed patch only for the same confirmed person and donor pair.

    The caller verifies physical image/mask artifacts and obtains current SQL
    consent, session and qualification through authority. No generated pixels.
    """
    if not callable(check_active) or not callable(authority):
        raise ValueError('GLARE_DONOR_INPUT_INVALID')
    owned = PortraitGlareParams.model_validate_json(canonical_bytes(params))
    raw = bytes(confirmation_raw)

    def authorize():
        check_active()
        if authority(owned.model_copy(deep=True), raw) is not True:
            raise ValueError('GLARE_DONOR_AUTHORITY_UNAVAILABLE')
        check_active()

    authorize()
    geometry = verify_glare_face_geometry(owned, raw, source_faces_raw,
                                          donor_faces_raw, source_size,
                                          donor_size, min_face_edge_px)
    authorize()
    result = prepare_donor_patch(source_patch, donor_rgb,
        geometry.source_landmarks, geometry.donor_landmarks,
        glasses_mask, protected, geometry.source_diagonal, owned.strength,
        source_origin, check_active, encoding=encoding,
        donor_alpha=donor_alpha, source_alpha=source_alpha)
    authorize()
    return result

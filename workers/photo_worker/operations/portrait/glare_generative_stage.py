"""V28 donorless glare confirmed patch to synthetic reviewed ImageResult."""

import numpy as np

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.v2.portrait_donor_images import verify_donor_working_image
from photo_worker.v2.portrait_donor_mask_patch import (
    plan_donor_mask_patch, select_donor_mask_patch,
)

from .glare_generative_evidence import verify_generative_glare_face
from .glare_generative_files import write_generative_glare_files
from .glare_generative_patch import prepare_generative_glare_patch
from .glare_generative_result import stage_generative_glare_result


def prepare_and_stage_generative_glare(params, source_raw, confirmation_raw,
                                        source_faces_raw, source_rgb,
                                        full_target, full_protected, engine,
                                        artifacts, scratch, *, min_face_edge_px,
                                        check_active, authority):
    """Create a synthetic candidate only after current consent/model authority.

    The caller owns a verified source RGB mapping and a qualified acquired
    inpaint_rgb engine for the full duration of this call.
    """
    if (not isinstance(params, PortraitGlareParams)
            or params.mode != 'generative' or not callable(check_active)
            or not callable(authority)):
        raise ValueError('GLARE_GENERATIVE_STAGE_INVALID')
    owned = PortraitGlareParams.model_validate_json(canonical_bytes(params))
    source_bytes = bytes(source_raw)
    proof = bytes(confirmation_raw)
    source = np.asarray(source_rgb)
    target = np.asarray(full_target)
    protected = np.asarray(full_protected)

    def admit():
        check_active()
        if authority(owned.model_copy(deep=True), proof) is not True:
            raise ValueError('GLARE_GENERATIVE_AUTHORITY_UNAVAILABLE')
        check_active()

    admit()
    image = verify_donor_working_image(source_bytes, owned.base)
    shape = (image.canvas.height, image.canvas.width)
    if (source.dtype != np.float32 or source.shape != (*shape, 3)
            or target.dtype != np.float32 or target.shape != shape
            or protected.dtype != np.float32 or protected.shape != shape):
        raise ValueError('GLARE_GENERATIVE_STAGE_INVALID')
    face = verify_generative_glare_face(owned, proof, source_faces_raw,
        (image.canvas.width, image.canvas.height), min_face_edge_px)
    origin, size = plan_donor_mask_patch(target, face.diagonal_px,
                                         check_active=admit)
    mask_patch, guard_patch = select_donor_mask_patch(target, protected,
        origin, size, check_active=admit)
    x, y = origin
    width, height = size
    admit()
    patch = prepare_generative_glare_patch(
        source[y:y+height, x:x+width], mask_patch, guard_patch,
        owned.strength, image.color.profile_id, engine,
        check_active=admit)
    with write_generative_glare_files(source, patch, origin, scratch,
            target=mask_patch, protected=guard_patch,
            check_active=admit) as files:
        admit()
        staged = stage_generative_glare_result(artifacts, owned,
            source_bytes, files, check_active=admit)
        admit()
    return staged

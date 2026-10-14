"""V28 physical donor glare patch to full-frame reviewed result."""

import numpy as np

from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.portrait_restoration import PortraitGlareParams
from photo_worker.v2.portrait_donor_material import read_donor_image_material
from photo_worker.v2.portrait_donor_images import verify_donor_image_pair
from photo_worker.v2.portrait_donor_mask_patch import (
    plan_donor_mask_patch, select_donor_mask_patch,
)

from .donor_files import write_donor_patch_files
from .donor_resources import admit_donor_portrait
from .glare_donor import prepare_glare_donor_patch, verify_glare_face_geometry
from .glare_result import stage_glare_donor_result


def prepare_and_stage_glare_donor(params, source_raw, donor_raw,
                                  confirmation_raw, source_faces_raw,
                                  donor_faces_raw, reader, mask, protected,
                                  artifacts, scratch, *, source_origin,
                                  min_face_edge_px, material_scratch_root,
                                  material_max_bytes, check_active, authority,
                                  alpha_mode):
    """Bind physical arrays to one authorized donor patch and result closure.

    Caller must first prove current tenant/session/consent, masks and accepted
    operation origin. This function repeats authority across every IO boundary.
    """
    if (not isinstance(params, PortraitGlareParams) or params.mode != 'donor'
            or not callable(check_active) or not callable(authority)
            or not isinstance(mask, np.ndarray) or not isinstance(protected, np.ndarray)
            or mask.dtype != np.float32 or protected.dtype != np.float32
            or mask.ndim != 2 or mask.shape != protected.shape
            or min(mask.shape) < 1 or mask.size > 1_048_576
            or not np.isfinite(mask).all() or not np.isfinite(protected).all()
            or np.any(mask < 0) or np.any(mask > 1)
            or np.any(protected < 0) or np.any(protected > 1)
            or not isinstance(source_origin, tuple) or len(source_origin) != 2
            or any(type(n) is not int or n < 0 for n in source_origin)):
        raise ValueError('GLARE_DONOR_STAGE_INVALID')
    owned = PortraitGlareParams.model_validate_json(canonical_bytes(params))
    source = bytes(source_raw)
    donor = bytes(donor_raw)
    proof = bytes(confirmation_raw)
    source_faces = bytes(source_faces_raw)
    donor_faces = bytes(donor_faces_raw)
    target = mask.copy()
    guard = protected.copy()
    target.setflags(write=False)
    guard.setflags(write=False)

    def admit():
        check_active()
        if authority(owned.model_copy(deep=True), proof) is not True:
            raise ValueError('GLARE_DONOR_AUTHORITY_UNAVAILABLE')
        check_active()

    admit()
    with read_donor_image_material(owned, source, donor, reader,
            scratch_root=material_scratch_root, max_bytes=material_max_bytes,
            check_active=check_active, alpha_mode=alpha_mode) as material:
        x, y = source_origin
        height, width = target.shape
        if (x + width > material.source_rgb.shape[1]
                or y + height > material.source_rgb.shape[0]):
            raise ValueError('GLARE_DONOR_CANVAS')
        source_patch = material.source_rgb[y:y+height, x:x+width]
        source_alpha = (None if material.source_alpha is None else
                        material.source_alpha[y:y+height, x:x+width])
        patch = prepare_glare_donor_patch(owned, proof, source_faces,
            donor_faces, source_patch, material.donor_rgb, target, guard,
            (material.source_image.canvas.width, material.source_image.canvas.height),
            (material.donor_image.canvas.width, material.donor_image.canvas.height),
            source_origin, min_face_edge_px, check_active, authority,
            encoding=material.source_image.color.encoding,
            donor_alpha=material.donor_alpha, source_alpha=source_alpha)
        with write_donor_patch_files(material.source_rgb, patch, source_origin,
                scratch, target=target, protected=guard,
                check_active=check_active) as files:
            admit()
            staged = stage_glare_donor_result(artifacts, owned, source, files,
                color_warning=patch.color.warning, check_active=admit)
            admit()
    return staged


def prepare_and_stage_glare_full_mask(params, source_raw, donor_raw,
                                      confirmation_raw, source_faces_raw,
                                      donor_faces_raw, reader, full_target,
                                      full_protected, artifacts, scratch, *,
                                      profile, min_face_edge_px,
                                      material_scratch_root,
                                      material_max_bytes, check_active,
                                      authority, alpha_mode):
    """Plan a bounded halo patch covering every target pixel on the canvas."""
    if (not callable(authority) or not callable(check_active)
            or type(min_face_edge_px) is not int
            or not 32 <= min_face_edge_px <= 512):
        raise ValueError('GLARE_DONOR_AUTHORITY_UNAVAILABLE')
    owned = PortraitGlareParams.model_validate_json(canonical_bytes(params))
    proof = bytes(confirmation_raw)
    check_active()
    if authority(owned.model_copy(deep=True), proof) is not True:
        raise ValueError('GLARE_DONOR_AUTHORITY_UNAVAILABLE')
    source, donor = verify_donor_image_pair(owned, bytes(source_raw), bytes(donor_raw))
    if (not isinstance(full_target, np.ndarray) or not isinstance(full_protected, np.ndarray)
            or full_target.shape != (source.canvas.height, source.canvas.width)
            or full_protected.shape != full_target.shape):
        raise ValueError('GLARE_DONOR_MASK_GEOMETRY')
    geometry = verify_glare_face_geometry(owned, proof, source_faces_raw,
        donor_faces_raw, (source.canvas.width, source.canvas.height),
        (donor.canvas.width, donor.canvas.height),
        min_face_edge_px)
    origin, size = plan_donor_mask_patch(full_target,
        geometry.source_diagonal, check_active=check_active)
    target, protected = select_donor_mask_patch(full_target, full_protected,
        origin, size, check_active=check_active)
    required = admit_donor_portrait(profile, source.canvas.width,
        source.canvas.height, donor.canvas.width, donor.canvas.height, *size)
    if (type(material_max_bytes) is not int or
            material_max_bytes < required['material_scratch_bytes']):
        raise ValueError('GLARE_DONOR_SCRATCH_UNAVAILABLE')
    check_active()
    return prepare_and_stage_glare_donor(owned, source_raw, donor_raw,
        proof, source_faces_raw, donor_faces_raw, reader, target, protected,
        artifacts, scratch, source_origin=origin,
        min_face_edge_px=min_face_edge_px,
        material_scratch_root=material_scratch_root,
        material_max_bytes=material_max_bytes, check_active=check_active,
        authority=authority, alpha_mode=alpha_mode)

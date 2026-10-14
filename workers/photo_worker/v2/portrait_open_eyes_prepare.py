"""V28 evidence-bound patch path, not an installed worker operation."""
from photo_worker.algorithms.portrait_donor_patch import prepare_donor_patch
from photo_worker.v2.portrait_donor_evidence import verify_open_eyes_faces
from photo_worker.v2.portrait_donor_images import verify_donor_image_pair


def prepare_open_eyes_manifest_patch(params, source_image_raw, donor_image_raw,
                                    confirmation_raw, source_faces_raw, donor_faces_raw,
                                    source_patch, donor_rgb, mask, protected,
                                    source_origin, min_face_edge_px, check, authority, *, donor_alpha=None, source_alpha=None):
    """Derive canvas sizes and encoding from pinned manifests.

    Physical pixel/mask artifact verification remains the caller's responsibility.
    Pixel arrays use their manifest encoding; output retains that encoding.
    """
    if not callable(check) or not callable(authority):
        raise ValueError('DONOR_AUTHORITY')
    check()
    owned_params = params.model_copy(deep=True)
    source, donor = verify_donor_image_pair(
        owned_params, bytes(source_image_raw), bytes(donor_image_raw))
    check()
    return prepare_open_eyes_patch(
        owned_params, confirmation_raw, source_faces_raw, donor_faces_raw,
        source_patch, donor_rgb, mask, protected,
        (source.canvas.width, source.canvas.height),
        (donor.canvas.width, donor.canvas.height), source_origin,
        min_face_edge_px, check, authority, encoding=source.color.encoding,
        donor_alpha=donor_alpha, source_alpha=source_alpha)


def prepare_open_eyes_patch(params, confirmation_raw, source_faces_raw, donor_faces_raw,
                           source_patch, donor_rgb, mask, protected, source_size, donor_size,
                           source_origin, min_face_edge_px, check, authority, *, encoding='linear', donor_alpha=None, source_alpha=None):
    """Caller authority must check current lease/identity/consent/source scope.

    Mandatory callback returns exactly True; no stored proof caches permission.
    This function does not download/upload artifacts or validate mask raster bytes.
    """
    if not callable(check) or not callable(authority):
        raise ValueError('DONOR_AUTHORITY')
    check()
    # Own immutable documents before external callbacks.
    confirmation = bytes(confirmation_raw)
    source_faces = bytes(source_faces_raw)
    donor_faces = bytes(donor_faces_raw)
    owned_params = params.model_copy(deep=True)
    def admit():
        check()
        if authority(owned_params.model_copy(deep=True), confirmation) is not True:
            raise ValueError('DONOR_AUTHORITY')
        check()
    admit()
    geometry = verify_open_eyes_faces(owned_params, confirmation, source_faces, donor_faces,
                                     source_size, donor_size, min_face_edge_px)
    if donor_rgb.shape[:2] != (donor_size[1], donor_size[0]):
        raise ValueError('DONOR_CANVAS')
    x, y = source_origin
    if x + source_patch.shape[1] > source_size[0] or y + source_patch.shape[0] > source_size[1]:
        raise ValueError('DONOR_CANVAS')
    admit()
    result = prepare_donor_patch(source_patch, donor_rgb, geometry.source_landmarks,
                                 geometry.donor_landmarks, mask, protected,
                                 geometry.source_diagonal, owned_params.strength, source_origin, check,
                                 encoding=encoding, donor_alpha=donor_alpha, source_alpha=source_alpha)
    admit()
    return result

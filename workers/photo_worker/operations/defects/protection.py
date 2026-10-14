"""Verify materialized proposal protection against original face inputs."""
from collections.abc import Callable, Sequence
import numpy as np
from photo_worker.contracts.images import WorkingImage
from photo_worker.algorithms.beauty_roi import plan_roi
from photo_worker.algorithms.skin_mask import build_skin_mask
from photo_worker.operations.portrait.prepare import FaceInput



def plan_proposal_face(entry: FaceInput, *, max_roi_pixels: int,
                       check_active: Callable[[], None]):
    """Only degenerate geometry is a reportable skip; invalid masks still fail."""
    check_active()
    try:
        return plan_roi(entry.face, max_work_pixels=max_roi_pixels)
    except ValueError as exc:
        if exc.args != ('DEGENERATE_FACE_LANDMARKS',):
            raise
    h, w = entry.face.canvas.height, entry.face.canvas.width
    for array in (entry.skin_probability, entry.manual):
        if array is None:
            continue
        if array.shape != (h, w) or array.dtype != np.float32:
            raise ValueError('Invalid proposal protection face mask')
        for y in range(0, h, 256):
            for x in range(0, w, 256):
                check_active()
                tile = array[y:y+256, x:x+256]
                if not np.isfinite(tile).all() or np.any(tile < 0) or np.any(tile > 1):
                    raise ValueError('Invalid ROI probability mask')
    return None


def verify_proposal_protection(image: WorkingImage, faces: Sequence[FaceInput],
                               protection: np.ndarray, labels: np.ndarray, *,
                               permanent: np.ndarray | None = None,
                               protections: Sequence[np.ndarray] = (),
                               max_roi_pixels: int = 4194304,
                               check_active: Callable[[], None] = lambda: None) -> None:
    """Read-only, bounded check before any outputs are staged.

    Derive the exact feature raster using the same pinned OpenCV geometry as
    proposal preparation. Extra protection is permitted; missing protection
    and any candidate on even partially protected pixels are rejected. The
    caller must separately establish original artifact identity/qualification.
    """
    check_active()
    h, w = image.canvas.height, image.canvas.width
    if (h*w > 200000000 or protection.shape != (h, w)
        or protection.dtype != np.float32 or labels.shape != (h, w)
        or labels.dtype != np.uint16 or not protection.flags.c_contiguous
        or not labels.flags.c_contiguous):
        raise ValueError('Invalid proposal protection layout')
    if type(max_roi_pixels) is not int or not 1 <= max_roi_pixels <= 4194304:
        raise ValueError('Invalid proposal protection budget')
    if len(faces) > 64 or len({f.face.object_id for f in faces}) != len(faces):
        raise ValueError('Invalid proposal protection faces')
    protections = tuple(protections)
    if len(protections) > 64: raise ValueError('Protection input limit')
    sources = protections if permanent is None else (permanent, *protections)
    for source in sources:
        if source.shape != (h,w) or source.dtype != np.float32:
            raise ValueError('Invalid permanent protection layout')
    # Tiles bound temporaries even if a full image row exceeds the RAM budget.
    for y in range(0, h, 256):
        for x in range(0, w, 256):
            check_active()
            sl = (slice(y, y+256), slice(x, x+256))
            actual = protection[sl]
            if not np.isfinite(actual).all() or np.any(actual < 0) or np.any(actual > 1):
                raise ValueError('Invalid proposal protection values')
            if np.any((labels[sl] != 0) & (actual > 0)):
                raise ValueError('Proposal candidate intersects protection')
            for source in sources:
                check_active()
                required = source[sl]
                if (not np.isfinite(required).all() or np.any(required < 0)
                    or np.any(required > 1) or np.any(actual < required)):
                    raise ValueError('Proposal permanent protection missing')
    for entry in faces:
        check_active()
        if entry.face.canvas != image.canvas or entry.face.transform_chain != image.transform_chain:
            raise ValueError('Proposal protection face geometry mismatch')
        for array in (entry.skin_probability, entry.manual):
            if array is not None and (array.shape != (h, w) or array.dtype != np.float32):
                raise ValueError('Invalid proposal protection face mask')
        plan = plan_proposal_face(entry, max_roi_pixels=max_roi_pixels, check_active=check_active)
        if plan is None:
            continue
        r = plan.rect
        sl = (slice(r.y, r.y+r.height), slice(r.x, r.x+r.width))
        derived = build_skin_mask(entry.face, r,
            None if entry.skin_probability is None else entry.skin_probability[sl],
            manual=None if entry.manual is None else entry.manual[sl],
            manual_mode=entry.manual_mode, max_work_pixels=max_roi_pixels,
            check_active=check_active)
        if np.any(protection[sl] < derived.protected):
            raise ValueError('Proposal facial feature protection missing')
        del derived
    check_active()

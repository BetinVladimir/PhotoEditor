"""V28 admitted-patch composition; identity/consent/lineage remain caller gates."""
from dataclasses import dataclass
from collections.abc import Callable
import numpy as np
from photo_worker.algorithms.portrait_donor import (
    DonorAlignment, DonorColorMatch, align_local_patch, donor_mask_geometry,
    sample_donor_pixels, match_patch_color, compose_donor_patch,
)


@dataclass(frozen=True)
class DonorPatchResult:
    pixels: np.ndarray
    alignment: DonorAlignment
    color: DonorColorMatch
    changed_mask: np.ndarray
    requires_review: bool = True


def prepare_donor_patch(source_patch, donor_rgb, source68, donor68, mask, protected,
                        source_face_diagonal: float, strength: float,
                        source_origin: tuple[int, int], check: Callable[[], None],
                        *, encoding='linear', donor_alpha=None, source_alpha=None) -> DonorPatchResult:
    """Process a <=1M-pixel patch with caller-supplied filter halo.

    Origin is global source x,y; landmarks are global pixel coordinates.
    Callers crop the core and preserve encoded source/alpha outside it. This
    function is not a full-frame worker operation or admission authority.
    """
    if not callable(check) or encoding not in ('linear', 'gamma24'):
        raise ValueError('DONOR_CONTEXT')
    check()
    source = np.asarray(source_patch)
    if (source.ndim != 3 or source.shape[2] != 3 or source.dtype != np.float32 or
            min(source.shape[:2]) < 1 or source.shape[0] * source.shape[1] > 1_048_576 or
            not np.isfinite(source).all() or np.any(source < 0) or
            len(source_origin) != 2 or any(type(v) is not int or v < 0 for v in source_origin)):
        raise ValueError('DONOR_PATCH')
    if np.shape(mask) != source.shape[:2] or np.shape(protected) != source.shape[:2]:
        raise ValueError('DONOR_MASK')
    source_covered = np.ones(source.shape[:2],dtype=bool)
    if source_alpha is not None:
        alpha=np.asarray(source_alpha)
        if (alpha.dtype!=np.float32 or alpha.shape!=source.shape[:2] or
                not np.isfinite(alpha).all() or np.any(alpha<0) or np.any(alpha>1)):
            raise ValueError('DONOR_ALPHA')
        source_covered=alpha>1e-6
    if not np.isfinite(strength) or not 0 <= strength <= 1:
        raise ValueError('DONOR_STRENGTH')
    alignment = align_local_patch(source68, donor68, source_face_diagonal)
    check()
    geometry = donor_mask_geometry(mask, protected, source_face_diagonal)
    active = (geometry.feather > 0) & (np.asarray(protected) == 0) & (strength > 0)
    if not np.any(active):
        # No transfer round-trip or donor sampling for an exact disabled effect.
        pixels=source.copy()
        gains=np.ones(3,dtype=np.float64)
        changed=np.zeros(source.shape[:2],dtype=np.float32)
        pixels.setflags(write=False)
        gains.setflags(write=False)
        changed.setflags(write=False)
        check()
        return DonorPatchResult(pixels,alignment,DonorColorMatch(gains,0,None),changed)
    encoded_source = source
    requested = active | (geometry.color_ring & source_covered)
    if encoding == 'gamma24':
        # Only target/ring values participate in linear math. Untouched HDR
        # pixels must neither round-trip nor fail an unnecessary conversion.
        decoded = np.power(source[requested].astype(np.float64), 2.4)
        if np.any(decoded > np.finfo(np.float32).max):
            raise ValueError('DONOR_PIXELS')
        source = source.copy()
        source[requested] = decoded.astype(np.float32)
    y, x = np.nonzero(requested)
    coordinates = np.column_stack((x + source_origin[0], y + source_origin[1]))
    sampled, opacity = sample_donor_pixels(donor_rgb, alignment.matrix, coordinates,
        encoding=encoding, donor_alpha=donor_alpha, return_alpha=True)
    covered = opacity > 1e-6
    if np.any(active[y,x] & ~covered):
        raise ValueError('DONOR_ALPHA_COVERAGE')
    check()
    ring_positions = geometry.color_ring[y, x] & covered & source_covered[y,x]
    color = match_patch_color(source[y[ring_positions],x[ring_positions]], sampled[ring_positions])
    proposal = source.copy()
    proposal[y, x] = sampled
    check()
    pixels = compose_donor_patch(source, proposal, geometry.feather, protected, strength, color.gains)
    if encoding == 'gamma24':
        # Preserve exact encoded source bytes wherever composition has zero weight.
        encoded = encoded_source.copy()
        encoded[active] = np.power(pixels[active].astype(np.float64), 1 / 2.4).astype(np.float32)
        pixels = encoded
    check()
    changed=np.any(pixels.view(np.uint32)!=encoded_source.view(np.uint32),axis=2).astype(np.float32)
    changed.setflags(write=False)
    pixels.setflags(write=False)
    return DonorPatchResult(pixels, alignment, color, changed)

"""Numerical inpaint preview shared by distinct proposal contracts.

The caller supplies the closed operation model and its own admission callback.
No contract conversion, approval, publication or installed operation is created.
"""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.runtime.executor import OperationFailure
from .candidate_inpaint import compute_inpaint_region, inpaint_mask_roi
from .candidate_blend import blend_candidate_region
from .candidate_preservation import CandidatePreservation, verify_candidate_preservation


@dataclass(frozen=True)
class InpaintPreviewComputed:
    original: np.ndarray
    candidate: np.ndarray
    preservation: CandidatePreservation
    roi: tuple[int, int, int, int]
    global_pass: bool
    peak_bytes: int


def compute_inpaint_preview(arrays, params, engine, *, verify_admission,
        max_bytes, check_active, strength, input_model):
    """Use already byte-verified owned arrays and an admitted qualified engine.

    Admission must verify the exact model/runtime/device installation pins in
    params. This result carries no approval, publication or alpha evidence.
    """
    owned = input_model.model_validate_json(canonical_bytes(params))
    if not callable(verify_admission) or not callable(check_active):
        raise ValueError('SEMANTIC_PREVIEW_INSTALLATION_REQUIRED')
    if type(max_bytes) is not int or not 64*1048576 <= max_bytes <= 2**53-1:
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    def admit():
        check_active()
        if verify_admission(owned.model_copy(deep=True)) is not True:
            raise ValueError('SEMANTIC_PREVIEW_ADMISSION_DENIED')
        check_active()
    if (type(strength) not in (int, float) or not np.isfinite(strength)
            or not 0 <= strength <= 1):
        raise ValueError('SEMANTIC_REMOVAL_STRENGTH_INVALID')
    admit()
    source = arrays.original
    if not isinstance(source, np.ndarray) or source.ndim != 3 or source.shape[2] != 3:
        raise ValueError('SEMANTIC_CANDIDATE_ARRAYS_INVALID')
    h, w = source.shape[:2]
    # Includes owned loader arrays, output, full-frame global pass and ROI/delta
    # intermediates. Engine weights/device allocations require separate admission.
    if (type(arrays.loader_peak_bytes) is not int
            or type(arrays.retained_loader_bytes) is not int
            or not 20*h*w <= arrays.loader_peak_bytes <= max_bytes
            or not 0 <= arrays.retained_loader_bytes <= arrays.loader_peak_bytes):
        raise ValueError('SEMANTIC_REMOVAL_RESOURCE_PROFILE')
    peak = max(arrays.loader_peak_bytes, 512*1048576 + 256*h*w + arrays.retained_loader_bytes)
    if peak > max_bytes:
        raise OperationFailure('RESOURCE_OOM')
    verify_candidate_preservation(source, source, arrays.mask, arrays.protected,
        max_workspace_bytes=8*1048576, check_active=check_active)
    # Existing Big-LaMa adapter is SDR-only. Never silently clip HDR inputs.
    for row in range(0, h, 256):
        check_active()
        if np.any(source[row:row+256] > 1):
            raise ValueError('MODEL_ADAPTER_INPUT_INVALID')
    target = arrays.mask >= .5
    protected = arrays.protected > 0
    effective = target & ~protected
    roi = inpaint_mask_roi(effective, owned.context_pixels)
    x0, y0, x1, y1 = roi
    padded_pixels = ((y1-y0+7)//8*8)*((x1-x0+7)//8*8)
    # Global pass has maximum edge 1024 (minimum short edge 8).
    budget = max(padded_pixels, 1024*1024)
    admit()
    restored, weight, global_pass = compute_inpaint_region(engine, source,
        effective, protected, roi, feather_pixels=owned.feather_pixels,
        max_padded_pixels=budget, check_active=check_active)
    weight *= np.float32(strength)
    output = np.empty_like(source)
    for row in range(0, h, 256):
        check_active()
        output[row:row+256] = source[row:row+256]
    changed = blend_candidate_region(output, source[y0:y1, x0:x1], restored,
        weight, x0, y0, check_active)
    proof = verify_candidate_preservation(source, output, target, protected,
        max_workspace_bytes=8*1048576, check_active=check_active)
    if proof.changed_pixels != changed:
        raise ValueError('INPAINT_CHANGED_PIXELS_MISMATCH')
    admit()
    return InpaintPreviewComputed(source, output, proof, roi, global_pass, peak)



"""Measured numerical quality of an approved, color-qualified exposure."""
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.images import ColorSpace
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from photo_worker.operations.optical.color import to_working_linear
from photo_worker.operations.optical.contracts import ImageReport
from .local_exposure import local_exposure_memory, apply_local_exposure
from .exposure_encoding import restore_local_exposure_encoding


def measure_exposure_quality(edit, original, encoded, mask, protected, color, *,
        max_bytes, verify_color, check_active):
    if not callable(verify_color) or not callable(check_active):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    edit = ApprovedSemanticEdit.model_validate_json(canonical_bytes(edit))
    color = ColorSpace.model_validate_json(canonical_bytes(color))
    if (edit.kind != 'local_exposure' or not isinstance(original, np.ndarray)
            or original.dtype != np.float32 or original.ndim != 3 or original.shape[2] != 3
            or not isinstance(encoded, np.ndarray) or encoded.dtype != np.float32
            or encoded.shape != original.shape or any(not isinstance(value, np.ndarray)
                or value.dtype != np.float32 or value.shape != original.shape[:2]
                for value in (mask, protected))):
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    required = local_exposure_memory(original.shape[:2]) + 64*1024**2
    if type(max_bytes) is not int or not required <= max_bytes <= 2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    original=original.copy();encoded=encoded.copy();mask=mask.copy();protected=protected.copy()
    check_active()
    if verify_color(color.model_copy(deep=True)) is not True:
        raise ValueError('SEMANTIC_EXPOSURE_ICC_UNQUALIFIED')
    check_active()
    if color.profile_id != 'work_rec2020_gamma24_v1' or color.encoding != 'gamma24':
        raise ValueError('SEMANTIC_EXPOSURE_COLOR_UNSUPPORTED')
    for y in range(0, original.shape[0], 256):
        for x in range(0, original.shape[1], 256):
            check_active();tile=np.s_[y:y+256,x:x+256]
            before=original[tile];after=encoded[tile];weights=mask[tile];protection=protected[tile]
            if (not np.isfinite(before).all() or np.any(before<0)
                    or not np.isfinite(after).all() or np.any(after<0)):
                raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
            with np.errstate(under='ignore', divide='raise'):
                linear=to_working_linear(before,color)
            changed=apply_local_exposure(linear,weights,protection,ev=edit.ev,
                strength=edit.strength,max_bytes=96*256*256,check_active=check_active)
            expected=restore_local_exposure_encoding(before,changed,weights,protection,color,
                max_bytes=192*256*256,check_active=check_active)
            if not np.array_equal(expected.view(np.uint32),after.view(np.uint32)):
                raise ValueError('SEMANTIC_EXPOSURE_QUALITY_FAILED')
    check_active()
    return ImageReport(schema_version=1,status='pass',operation='semantic.local_exposure',
        skipped_count=0,reason=None,outside_mask_max_error=0.,clipping_fraction=0.,
        identity=None,transform=None,sources=[edit.source.model_copy(deep=True)],registrations=[])

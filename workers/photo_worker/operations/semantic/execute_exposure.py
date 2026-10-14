"""Installed local exposure execution boundary, without task publication.

Admission verifier must prove committed human batch/child/task/source authority.
Loader must admit decoding/source costs against max_bytes before large reads,
and prove exact pinned RGB/mask/transform/protection bytes and ICC.
Returned loader peak and retained-owner costs are mandatory; executor checks
the complete budget before copying arrays and subtracts retained owners from
the kernel budget. Loader must independently enforce its peak before IO.
Neither dependency has an implicit permissive default.
"""
from dataclasses import dataclass
import numpy as np
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic import ApprovedSemanticEdit
from .local_exposure import apply_local_exposure,local_exposure_memory,local_exposure_execution_memory
from .approved_binding import bind_approved_edit_documents


@dataclass(frozen=True)
class LocalExposureArrays:
    rgb_linear:np.ndarray
    mask:np.ndarray
    protected:np.ndarray
    loader_peak_bytes:int
    retained_loader_bytes:int


def execute_local_exposure(edit,candidate_raw,intent_raw,*,verify_admission,load_verified_arrays,max_bytes,check_active):
    if not all(callable(value) for value in (verify_admission,load_verified_arrays,check_active)):
        raise ValueError('SEMANTIC_EXPOSURE_INSTALLATION_REQUIRED')
    raw=canonical_bytes(edit)
    owned=ApprovedSemanticEdit.model_validate_json(raw)
    if owned.kind!='local_exposure':
        raise ValueError('SEMANTIC_EXPOSURE_KIND_INVALID')
    owned,_,_=bind_approved_edit_documents(owned,candidate_raw,intent_raw)
    if type(max_bytes) is not int or not 1<=max_bytes<=2**53-1:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    def admitted():
        check_active()
        if verify_admission(ApprovedSemanticEdit.model_validate_json(raw)) is not True:
            raise ValueError('SEMANTIC_EXPOSURE_ADMISSION_DENIED')
    admitted()
    arrays=load_verified_arrays(ApprovedSemanticEdit.model_validate_json(raw),max_bytes=max_bytes)
    if (not isinstance(arrays,LocalExposureArrays) or not isinstance(arrays.rgb_linear,np.ndarray)
            or arrays.rgb_linear.dtype!=np.float32 or arrays.rgb_linear.ndim!=3 or arrays.rgb_linear.shape[2]!=3
            or any(not isinstance(value,np.ndarray) or value.dtype!=np.float32
                   or value.shape!=arrays.rgb_linear.shape[:2] for value in (arrays.mask,arrays.protected))):
        raise ValueError('SEMANTIC_EXPOSURE_ARRAYS_INVALID')
    shape=arrays.rgb_linear.shape[:2]
    kernel_cost=local_exposure_memory(shape)
    required=local_exposure_execution_memory(shape,loader_peak_bytes=arrays.loader_peak_bytes,
        retained_loader_bytes=arrays.retained_loader_bytes)
    # Loader peak includes resident RGB/mask/protection, even for mapped arrays.
    if arrays.loader_peak_bytes<kernel_cost//96*20 or required>max_bytes:
        raise ValueError('SEMANTIC_EXPOSURE_RESOURCE_PROFILE')
    kernel_budget=max_bytes-arrays.retained_loader_bytes
    # Own loaded arrays before the next authority callback can change aliases.
    rgb=arrays.rgb_linear.copy();mask=arrays.mask.copy();protected=arrays.protected.copy()
    admitted()
    output=apply_local_exposure(rgb,mask,protected,ev=owned.ev,strength=owned.strength,
                                max_bytes=kernel_budget,check_active=check_active)
    admitted()
    return output

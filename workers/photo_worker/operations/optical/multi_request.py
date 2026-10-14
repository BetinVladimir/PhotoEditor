"""Frozen V29 Params/Calibration boundary; qualification authority is separate."""
from contextlib import contextmanager
from dataclasses import dataclass
from hashlib import sha256
import re
from photo_worker.contracts.base import ArtifactRef,canonical_bytes
from photo_worker.contracts.optical_multi import OpticalPanoramaParams,OpticalFocusStackParams
from photo_worker.operations.session.contracts import Calibration
from .multi_image_prepare import prepare_multi_image


@dataclass(frozen=True)
class FrozenMultiRequest:
    operation:str
    params_jcs:bytes
    calibration_jcs:bytes
    model_lock_sha:str
    dataset_sha:str
    max_pixels:int


def decode_multi_request(operation,params_raw,calibration_raw,*,model_lock_sha,dataset_sha,max_pixels):
    """Freeze exact canonical bytes and installed qualification identity pins.

    `qualified=true` is checked but is not accepted as qualification proof. The
    operation adapter must independently verify signed/imported evidence, the
    runtime lock, current lease/source authority and exact calibration ref.
    """
    if (operation not in ('optical.panorama','optical.focus_stack')
            or not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536
            or not isinstance(calibration_raw,bytes) or not 2<=len(calibration_raw)<=1048576
            or not isinstance(model_lock_sha,str) or re.fullmatch('[0-9a-f]{64}',model_lock_sha) is None
            or not isinstance(dataset_sha,str) or re.fullmatch('[0-9a-f]{64}',dataset_sha) is None
            or type(max_pixels) is not int or not 1<=max_pixels<=200000000):
        raise ValueError('MULTI_REQUEST_INVALID')
    model=OpticalPanoramaParams if operation=='optical.panorama' else OpticalFocusStackParams
    params=model.model_validate_json(params_raw)
    calibration=Calibration.model_validate_json(calibration_raw)
    if canonical_bytes(params)!=params_raw or canonical_bytes(calibration)!=calibration_raw:
        raise ValueError('MULTI_REQUEST_NONCANONICAL')
    if (sha256(calibration_raw).hexdigest()!=params.calibration.sha256 or not calibration.qualified
            or calibration.model_lock_sha!=model_lock_sha or calibration.dataset_sha!=dataset_sha):
        raise ValueError('MULTI_CALIBRATION_MISMATCH')
    if params.max_pixels>max_pixels:raise ValueError('MULTI_PIXEL_LIMIT')
    return FrozenMultiRequest(operation,params_raw,calibration_raw,model_lock_sha,dataset_sha,max_pixels)


@contextmanager
def prepare_frozen_multi_request(request,images,rgbs,alphas,scratch,*,verify_icc,ram_max_bytes,
                                 tile=256,check_active=lambda:None):
    if not isinstance(request,FrozenMultiRequest):raise ValueError('MULTI_REQUEST_INVALID')
    check_active()
    owned=decode_multi_request(request.operation,request.params_jcs,request.calibration_jcs,
        model_lock_sha=request.model_lock_sha,dataset_sha=request.dataset_sha,max_pixels=request.max_pixels)
    model=OpticalPanoramaParams if owned.operation=='optical.panorama' else OpticalFocusStackParams
    params=model.model_validate_json(owned.params_jcs);calibration=Calibration.model_validate_json(owned.calibration_jcs)
    if not isinstance(images,(list,tuple)) or len(images)!=len(params.frames):raise ValueError('MULTI_SOURCE_MISMATCH')
    with prepare_multi_image(images,params.frames,rgbs,alphas,params.reference_ordinal,scratch,
            operation='panorama' if isinstance(params,OpticalPanoramaParams) else 'focus_stack',
            projection=params.projection if isinstance(params,OpticalPanoramaParams) else 'planar',
            motion_policy=params.motion_policy if isinstance(params,OpticalFocusStackParams) else 'review',
            max_pixels=params.max_pixels,ram_max_bytes=ram_max_bytes,verify_icc=verify_icc,
            registration_error_px=calibration.registration_error_px,match_inlier_min=calibration.match_inlier_min,
            motion_threshold=calibration.motion_threshold,tile=tile,check_active=check_active) as prepared:
        check_active();yield prepared

"""Attempt-owned original source to qualified negative files bridge."""
from contextlib import contextmanager
from copy import deepcopy
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.contracts.images import WorkingImage
from .negative_attempt import negative_attempt_checkpoint
from .negative_source import load_negative_source_arrays
from .negative_qualified_prepare import prepare_qualified_negative_files
from .adoption import AdoptionInvalid


@contextmanager
def prepare_leased_negative_files(reader,database,lease,params_raw,source_raw,
        calibration_raw,report_raw,policy,scratch,*,runtime_lock,runtime_sha,
        source_max_bytes,ram_max_bytes,qualification_authority,verify_color,check_active):
    """Reserve combined scratch before IO and retain live gates through use.

    Installation supplies an original declared-input reader, current qualified
    evidence authority and trusted ICC validator. This context does not publish
    or adopt output, and all files expire when the context exits.
    """
    if (not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536
            or not isinstance(source_raw,bytes) or not 2<=len(source_raw)<=65536
            or not isinstance(calibration_raw,(bytes,bytearray)) or not 2<=len(calibration_raw)<=65536
            or not isinstance(report_raw,(bytes,bytearray)) or not 2<=len(report_raw)<=8<<20):
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    raw=bytes(params_raw);source=bytes(source_raw)
    calibration=bytes(calibration_raw) if isinstance(calibration_raw,(bytes,bytearray)) else calibration_raw
    report=bytes(report_raw) if isinstance(report_raw,(bytes,bytearray)) else report_raw
    params=OpticalNegativeParams.model_validate_json(raw)
    image=WorkingImage.model_validate_json(source)
    from photo_worker.operations.optical.negative_resources import negative_resources,admit_negative
    required=negative_resources(image.canvas.width,image.canvas.height,has_alpha=image.alpha is not None)
    if type(ram_max_bytes) is not int or not 1<=ram_max_bytes<=2**53-1:
        raise AdoptionInvalid('NEGATIVE_RESOURCE_INVALID')
    if ram_max_bytes<required['ram_bytes']:raise AdoptionInvalid('NEGATIVE_RAM_EXCEEDED')
    owned=deepcopy(lease);trusted=deepcopy(policy);runtime=deepcopy(runtime_lock)
    admitted=admit_negative(owned.execution_profile,image.canvas.width,image.canvas.height,
        has_alpha=image.alpha is not None)
    ram_max_bytes=min(ram_max_bytes,admitted['ram_bytes'])
    pin=deepcopy(owned.execution_profile.get('negative_qualification'))
    if runtime_sha!=owned.execution_profile.get('negative_runtime_sha'):
        raise AdoptionInvalid('NEGATIVE_QUALIFICATION_PROFILE_INVALID')
    active=negative_attempt_checkpoint(database,owned,raw,qualification_authority,check_active)
    pixels=image.canvas.height*image.canvas.width
    if pixels>200_000_000 or type(source_max_bytes) is not int or not 1<=source_max_bytes<=2**53-1:
        raise AdoptionInvalid('NEGATIVE_RESOURCE_INVALID')
    prepared_bytes=16*pixels+32768
    if source_max_bytes+prepared_bytes>scratch.budget_bytes:
        raise AdoptionInvalid('NEGATIVE_SCRATCH_REQUIRED')
    if source_max_bytes>admitted['material_scratch_bytes'] or source_max_bytes+prepared_bytes>admitted['scratch_bytes']:
        raise AdoptionInvalid('NEGATIVE_SCRATCH_REQUIRED')
    def current():
        active();return deepcopy(pin)
    active()
    with scratch.reserve(source_max_bytes):
        with load_negative_source_arrays(reader,database,owned,raw,source,
                scratch_root=scratch.directory,max_bytes=source_max_bytes,ram_max_bytes=ram_max_bytes,
                verify_color=verify_color,check_active=active) as scan:
            with prepare_qualified_negative_files(scan,params,calibration,report,trusted,pin,scratch,
                    input_icc_sha=image.color.icc_sha256,runtime_lock=runtime,runtime_sha=runtime_sha,
                    ram_max_bytes=ram_max_bytes,load_current_pin=current,check_active=active) as files:
                yield files

"""Qualified leased negative preparation and complete output staging."""
from copy import deepcopy
from photo_worker.contracts.optical_negative import OpticalNegativeParams
from photo_worker.contracts.images import WorkingImage
from photo_worker.operations.optical.negative_resources import admit_negative
from photo_worker.operations.optical.negative_stage import stage_negative_result
from .negative_attempt import negative_attempt_checkpoint
from .negative_source import negative_source_authority
from .negative_leased_prepare import prepare_leased_negative_files
from .adoption import AdoptionInvalid


def prepare_and_stage_leased_negative(reader,artifacts,database,lease,params_raw,
        source_raw,calibration_raw,report_raw,policy,scratch,*,runtime_lock,runtime_sha,
        source_max_bytes,ram_max_bytes,qualification_authority,verify_color,
        check_active,clipping_fraction=None):
    """Return complete review closure only after all context exit gates succeed.

    Staged objects remain unadopted on failure and follow existing attempt GC.
    Output clipping is computed from pixels; an optional expected value must
    match it. Original task reader/current authority and atomic adoption remain
    installer responsibilities; independent pixel/report replay is separate.
    """
    if clipping_fraction is not None and type(clipping_fraction) not in (float,int) or clipping_fraction is not None and not 0<=clipping_fraction<=1:
        raise ValueError('NEGATIVE_STAGING_INVALID')
    owned=deepcopy(lease)
    if (not isinstance(params_raw,bytes) or not 2<=len(params_raw)<=65536
            or not isinstance(source_raw,bytes) or not 2<=len(source_raw)<=65536):
        raise ValueError('NEGATIVE_PARAMS_INVALID')
    params=OpticalNegativeParams.model_validate_json(params_raw)
    image=WorkingImage.model_validate_json(source_raw)
    admitted=admit_negative(owned.execution_profile,image.canvas.width,image.canvas.height,
        has_alpha=image.alpha is not None)
    attempt=negative_attempt_checkpoint(database,owned,params_raw,qualification_authority,check_active)
    source=negative_source_authority(database,owned,params_raw,verify_color=verify_color,check_active=check_active)
    def active():
        attempt();source(params,source_raw);attempt()
    with prepare_leased_negative_files(reader,database,owned,params_raw,source_raw,
            calibration_raw,report_raw,policy,scratch,runtime_lock=runtime_lock,runtime_sha=runtime_sha,
            source_max_bytes=source_max_bytes,ram_max_bytes=ram_max_bytes,
            qualification_authority=qualification_authority,verify_color=verify_color,
            check_active=check_active) as files:
        result=stage_negative_result(artifacts,params,source_raw,files,
            clipping_fraction=files.clipping_fraction if clipping_fraction is None else clipping_fraction,check_active=active)
        if result.bytes_written>admitted['output_bytes']:
            raise AdoptionInvalid('NEGATIVE_OUTPUT_EXCEEDED')
        active()
    active()
    return result

"""Own CPU beauty-filter qualification floor; live issuer/object authority is external."""
from .adoption import AdoptionInvalid
from photo_worker.contracts.base import canonical_bytes,parse_json
from .retouch_qualification_pin import RetouchQualificationPin
from .semantic_qualification_report import _verify_qualification_report

RETOUCH_REQUIRED_CHECKS=frozenset((
    'retouch.real_photo_review','retouch.outside_mask_exact','retouch.protected_pixels_exact',
    'retouch.alpha_preserved','retouch.strength_zero_exact','retouch.strength_once',
    'retouch.texture_keep','retouch.full_face_roi','retouch.landmark_source',
    'retouch.model_runtime_pins','retouch.cancellation','retouch.resource_admission','retouch.hdr_refusal',
))


def verify_retouch_qualification_report(raw,pin,policy,*,check_active):
    if type(pin) is not RetouchQualificationPin or not callable(check_active):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    # The mandatory retouch floor must use the same owned policy as report
    # verification; activity callbacks may mutate the caller's dictionary.
    try:
        owned_policy=parse_json(canonical_bytes(policy),8*1024*1024)
    except (ValueError,TypeError,OverflowError,UnicodeError) as error:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID') from error
    report=_verify_qualification_report(raw,pin,owned_policy,operation='semantic.preview_retouch_region',
        backend='cpu',check_active=check_active)
    if (not RETOUCH_REQUIRED_CHECKS<=set(owned_policy['required_checks'])
            or set(report['input_fixture_shas'])!=set(owned_policy['test_fixture_shas'])
            or type(report['metrics']['peak_vram_bytes']) is not int
            or report['metrics']['peak_vram_bytes']!=0):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID')
    check_active()
    return report

"""Removal-specific exact report verification; live SQL authority is separate."""
from .adoption import AdoptionInvalid
from photo_worker.contracts.base import canonical_bytes, parse_json
from .removal_qualification_pin import RemovalQualificationPin
from .semantic_qualification_report import _verify_qualification_report

REMOVAL_REQUIRED_CHECKS = frozenset((
    'removal.real_photo_review', 'removal.outside_mask_exact',
    'removal.protected_pixels_exact', 'removal.alpha_preserved',
    'removal.strength_zero_exact', 'removal.working_color_delta',
    'removal.global_pass', 'removal.model_runtime_pins',
    'removal.cancellation', 'removal.resource_admission',
))


def verify_removal_qualification_report(raw, pin, policy, *, check_active):
    if type(pin) is not RemovalQualificationPin or not callable(check_active):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    # Own the policy before any authority callback can mutate caller state.
    try:
        owned_policy = parse_json(canonical_bytes(policy), 8*1024*1024)
    except (ValueError, TypeError, OverflowError, UnicodeError) as error:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID') from error
    report = _verify_qualification_report(raw, pin, owned_policy,
        operation='semantic.preview_remove_object', backend=pin.backend,
        check_active=check_active)
    # An installation policy may add checks; it cannot drop the removal floor.
    if (not REMOVAL_REQUIRED_CHECKS <= set(owned_policy['required_checks'])
            or set(report['input_fixture_shas']) != set(owned_policy['test_fixture_shas'])):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID')
    peak = report['metrics']['peak_vram_bytes']
    if (type(peak) is not int or (pin.backend == 'cpu' and peak != 0)
            or (pin.backend == 'cuda' and peak > 10_000_000_000)):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID')
    # Metal has no fixed 10 GB cap. Deployment still admits measured memory.
    check_active()
    return report

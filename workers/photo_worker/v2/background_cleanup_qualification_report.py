"""BackgroundCleanup-specific exact report verification; live SQL authority is separate."""
from .adoption import AdoptionInvalid
from photo_worker.contracts.base import canonical_bytes, parse_json
from .background_cleanup_qualification_pin import BackgroundCleanupQualificationPin
from .semantic_qualification_report import _verify_qualification_report

BACKGROUND_CLEANUP_REQUIRED_CHECKS = frozenset((
    'background_cleanup.real_photo_review', 'background_cleanup.outside_mask_exact',
    'background_cleanup.protected_pixels_exact', 'background_cleanup.alpha_preserved',
    'background_cleanup.strength_zero_exact', 'background_cleanup.working_color_delta',
    'background_cleanup.global_pass', 'background_cleanup.model_runtime_pins',
    'background_cleanup.cancellation', 'background_cleanup.resource_admission',
))


def verify_background_cleanup_qualification_report(raw, pin, policy, *, check_active):
    if type(pin) is not BackgroundCleanupQualificationPin or not callable(check_active):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    # Own the policy before any authority callback can mutate caller state.
    try:
        owned_policy = parse_json(canonical_bytes(policy), 8*1024*1024)
    except (ValueError, TypeError, OverflowError, UnicodeError) as error:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID') from error
    report = _verify_qualification_report(raw, pin, owned_policy,
        operation='semantic.preview_background_cleanup', backend=pin.backend,
        check_active=check_active)
    # An installation policy may add checks; it cannot drop the background_cleanup floor.
    if (not BACKGROUND_CLEANUP_REQUIRED_CHECKS <= set(owned_policy['required_checks'])
            or set(report['input_fixture_shas']) != set(owned_policy['test_fixture_shas'])):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID')
    peak = report['metrics']['peak_vram_bytes']
    if (type(peak) is not int or (pin.backend == 'cpu' and peak != 0)
            or (pin.backend == 'cuda' and peak > 10_000_000_000)):
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_REPORT_INVALID')
    # Metal has no fixed 10 GB cap. Deployment still admits measured memory.
    check_active()
    return report

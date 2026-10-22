"""Distinct CPU byte-copy qualification; exposure-render reports cannot admit it."""
from .exposure_qualification_pin import ExposureQualificationPin
from .semantic_qualification_report import _verify_qualification_report
from .adoption import AdoptionInvalid


class ShownBackgroundCleanupQualificationPin(ExposureQualificationPin):
    """Same frozen pin wire schema, separate operation/report type."""


def verify_shown_background_cleanup_qualification_report(raw,pin,policy,*,check_active):
    if type(pin) is not ShownBackgroundCleanupQualificationPin:
        raise AdoptionInvalid('SEMANTIC_QUALIFICATION_PIN_INVALID')
    return _verify_qualification_report(raw,pin,policy,
        operation='semantic.adopt_shown_background_cleanup',backend='cpu',check_active=check_active)

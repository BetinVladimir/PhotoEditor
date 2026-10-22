"""Retouch shown-copy qualification has a distinct operation and pin type."""
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.shown_retouch_qualification import verify_shown_retouch_qualification_report


def test_retouch_copy_rejects_untyped_pin_before_report_io():
    with pytest.raises(AdoptionInvalid, match='PIN_INVALID'):
        verify_shown_retouch_qualification_report(b'{}', object(), {}, check_active=lambda: True)

"""BackgroundCleanup-copy qualification cannot borrow another operation's pin."""
import pytest
from photo_worker.v2.adoption import AdoptionInvalid
from photo_worker.v2.shown_background_cleanup_qualification import verify_shown_background_cleanup_qualification_report


def test_background_cleanup_copy_report_rejects_untyped_pin_before_parsing():
    with pytest.raises(AdoptionInvalid, match='PIN_INVALID'):
        verify_shown_background_cleanup_qualification_report(b'{}', object(), {}, check_active=lambda: None)

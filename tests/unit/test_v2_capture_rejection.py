from types import SimpleNamespace

import pytest

from photo_worker.v2.adoption_service import _capture_rejection_code


def failure(state, message):
    return SimpleNamespace(sqlstate=state, diag=SimpleNamespace(message_primary=message))


@pytest.mark.parametrize('message,code', [
    ('V24_IDENTITY_CONSENT_REVOKED', 'CONSENT_REVOKED'),
    ('V24_CAPTURE_CONTEXT_STALE', 'CAPTURE_CONTEXT_STALE'),
    ('CAPTURE_CONSENT_EVIDENCE_MISSING', 'CAPTURE_CONSENT_EVIDENCE_MISSING'),
])
def test_capture_known_invariant_failures_are_permanent(message, code):
    assert _capture_rejection_code(failure('23514', message)) == code


@pytest.mark.parametrize('state,message', [
    ('40001', 'V24_IDENTITY_CONSENT_REVOKED'),
    ('40P01', 'V24_CAPTURE_CONTEXT_STALE'),
    ('08006', 'connection unavailable'),
    ('23514', 'private database failure details'),
    (None, None),
])
def test_unknown_and_transient_database_errors_are_not_rejected(state, message):
    assert _capture_rejection_code(failure(state, message)) is None

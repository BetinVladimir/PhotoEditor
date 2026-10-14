"""Concrete revoke lifecycle and consent-gated recipe access observations."""
from dataclasses import dataclass
from photo_worker.runtime.restore_revoke_lifecycle import (
    RestoreConsentRevokeTarget, RestoreConsentRevokeReceipt, revoke_fixture_learning_consent)
from photo_worker.runtime.restore_revoke_access import (
    RestoreRevokeDocumentTarget, observe_fixture_revoke_document)
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeConsentEvidence, observe_fixture_learning_consent
from photo_worker.runtime.restore_access_probe import RestoreAccessReadableEvidence, RestoreAccessDenialEvidence
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRevokeObservation:
    schema_version: str
    baseline_access: RestoreAccessReadableEvidence
    baseline_consent: RestoreRevokeConsentEvidence
    revoke_receipt: RestoreConsentRevokeReceipt
    committed_consent: RestoreRevokeConsentEvidence
    denied_access: RestoreAccessDenialEvidence
    final_consent: RestoreRevokeConsentEvidence


def observe_fixture_revoke_lifecycle(connection, revoke_target, document_target, *, expected_database,
                                     tls_context, assert_installed_fixture, check_active):
    """Keep one installed fixture/identity across baseline, mutation and observations.

    This is not full restore acceptance: processing admission and races require
    separate evidence. No automatic retry on ambiguous mutation outcome.
    """
    if (not isinstance(revoke_target, RestoreConsentRevokeTarget)
            or not isinstance(document_target, RestoreRevokeDocumentTarget)
            or revoke_target.fixture != document_target.fixture
            or revoke_target.origin != document_target.access.origin
            or revoke_target.authorization != document_target.access.authorization
            or not callable(assert_installed_fixture) or not callable(check_active)):
        raise ValueError('RESTORE_REVOKE_LIFECYCLE_BINDING')
    def guard():
        require_restore_fixture(check_active,assert_installed_fixture)
    def scope(_): guard()
    common = dict(expected_database=expected_database, tls_context=tls_context,
        assert_fixture_scope=scope, check_active=check_active)
    guard()
    before = observe_fixture_revoke_document(connection, document_target, revoked=False, **common)
    guard()
    baseline, receipt, committed = revoke_fixture_learning_consent(connection, revoke_target, **common)
    guard()
    denied = observe_fixture_revoke_document(connection, document_target, revoked=True, **common)
    guard()
    final = observe_fixture_learning_consent(connection, revoke_target.fixture, expected_database=expected_database,
        revoked=True, assert_fixture_scope=scope, check_active=check_active)
    if final != committed:
        raise ValueError('RESTORE_REVOKE_LIFECYCLE_CHANGED')
    guard()
    return RestoreRevokeObservation('restore-revoke-observation/v1', before, baseline, receipt, committed, denied, final)

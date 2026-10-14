from dataclasses import replace
from uuid import uuid4
import ssl
import pytest
from photo_worker.runtime import restore_revoke_observation as module
from photo_worker.runtime.restore_access_probe import RestoreAccessTarget
from photo_worker.runtime.restore_revoke_access import RestoreRevokeDocumentTarget
from photo_worker.runtime.restore_revoke_lifecycle import RestoreConsentRevokeTarget
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode', ['valid', 'foreign_fixture', 'foreign_origin', 'foreign_credential',
    'baseline_failure', 'revoke_failure', 'denial_failure', 'changed_final', 'scope_failure','fixture_false','activity_false'])
def test_bound_revoke_lifecycle_stops_on_missing_or_changed_evidence(monkeypatch, mode):
    f = RestoreRevokeFixture(uuid4(), uuid4(), uuid4(), 'learning_person', uuid4(), 3, 5, 'v1', uuid4(), uuid4())
    target = RestoreConsentRevokeTarget('https://restore.test', f, 'revoke-key', 'Bearer private')
    access = RestoreAccessTarget(target.origin, f'/api/v1/tenants/{f.tenant_id}/v2-documents/{uuid4()}',
        '/control', target.authorization, 409, 'PROFILE_CONSENT_REVOKED', 'b' * 64)
    document = RestoreRevokeDocumentTarget(f, uuid4(), 'a' * 64, access)
    if mode == 'foreign_fixture': document = replace(document, fixture=replace(f, consent_id=uuid4()))
    if mode == 'foreign_origin': document = replace(document, access=replace(access, origin='https://other.test'))
    if mode == 'foreign_credential': document = replace(document, access=replace(access, authorization='Bearer other'))
    events = []
    def step(name, kwargs):
        kwargs['assert_fixture_scope'](None)
        events.append(name)
        if mode == name + '_failure': raise ValueError(name)
    def doc(connection, actual, *, revoked, **kwargs):
        assert actual is document
        step('denial' if revoked else 'baseline', kwargs)
        return 'denied' if revoked else 'readable'
    def revoke(connection, actual, **kwargs):
        assert actual is target
        step('revoke', kwargs)
        return 'baseline-consent', 'receipt', 'committed'
    def final(*args, **kwargs):
        step('final', kwargs)
        return 'changed' if mode == 'changed_final' else 'committed'
    monkeypatch.setattr(module, 'observe_fixture_revoke_document', doc)
    monkeypatch.setattr(module, 'revoke_fixture_learning_consent', revoke)
    monkeypatch.setattr(module, 'observe_fixture_learning_consent', final)
    def scope():
        if mode == 'scope_failure': raise RuntimeError('fixture unavailable')
        if mode == 'fixture_false': return False
    def run():
        return module.observe_fixture_revoke_lifecycle(object(), target, document,
            expected_database='isolated', tls_context=ssl.create_default_context(),
            assert_installed_fixture=scope, check_active=lambda: False if mode == 'activity_false' else None)
    if mode == 'valid':
        result = run()
        assert result.denied_access == 'denied' and result.final_consent == result.committed_consent
    else:
        with pytest.raises((ValueError, RuntimeError)): run()
    if mode.startswith('foreign_') or mode in ('scope_failure','fixture_false','activity_false'): assert not events
    assert events == ['baseline', 'revoke', 'denial', 'final'][:len(events)]

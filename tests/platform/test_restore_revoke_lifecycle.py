from dataclasses import replace
from types import SimpleNamespace
from uuid import uuid4
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.runtime import restore_revoke_lifecycle as module
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode', ['valid', 'replayed', 'conflict', 'redirect', 'network',
    'foreign_tenant', 'wrong_epoch', 'wrong_actor', 'numeric_revision', 'extra_envelope',
    'bad_key', 'long_policy', 'scope', 'scope_false', 'activity_false'])
def test_native_revoke_cas_receipt_and_no_retry(monkeypatch, mode):
    f = RestoreRevokeFixture(uuid4(), uuid4(), uuid4(), 'learning_person', uuid4(), 3, 5, 'v1', uuid4(), uuid4())
    if mode == 'long_policy': f = replace(f, policy_version='x' * 81)
    target = module.RestoreConsentRevokeTarget('https://restore.test', f,
        'bad key' if mode == 'bad_key' else 'restore-revoke-1', 'Bearer private')
    calls = []; closed = []
    data = dict(id=str(f.consent_id), tenant_id=str(f.tenant_id), project_id=str(f.project_id),
        resource_type='consent_grants', revision='6', epoch='4', purpose=f.purpose,
        person_id=str(f.person_id), status='revoked', policy_version=f.policy_version, actor_id=str(f.actor_id))
    if mode == 'foreign_tenant': data['tenant_id'] = str(uuid4())
    if mode == 'wrong_epoch': data['epoch'] = '5'
    if mode == 'wrong_actor': data['actor_id'] = str(uuid4())
    value = dict(id=str(f.consent_id), revision=6 if mode == 'numeric_revision' else '6', document=None, data=data)
    if mode == 'extra_envelope': value['unexpected'] = True
    class Connection:
        def __init__(self, *args, **kwargs): assert kwargs['timeout'] == 5
        def request(self, method, path, body, headers):
            calls.append(method)
            assert method == 'POST' and path == f'/api/v1/tenants/{f.tenant_id}/projects/{f.project_id}/consents'
            assert headers['Idempotency-Key'] == target.idempotency_key and headers['X-Photo-Client'] == 'native'
            assert parse_json(body) == dict(expected_revision='5', purpose=f.purpose,
                person_id=str(f.person_id), action='revoke', policy_version='v1')
            if mode == 'network': raise OSError('private credential')
        def getresponse(self):
            return SimpleNamespace(status=409 if mode == 'conflict' else 302 if mode == 'redirect' else 200,
                read=lambda _: canonical_bytes(value), getheader=lambda name, default=None:
                'application/json' if name == 'Content-Type' else
                'true' if name == 'Idempotency-Replayed' and mode == 'replayed' else default)
        def close(self): closed.append(True)
    monkeypatch.setattr(module.http.client, 'HTTPSConnection', Connection)
    def scope(actual):
        assert actual is target
        if mode == 'scope': raise RuntimeError('fixture changed')
        if mode == 'scope_false': return False
    def run():
        return module.request_fixture_consent_revoke(target, tls_context=ssl.create_default_context(),
            assert_fixture_scope=scope, check_active=lambda: False if mode == 'activity_false' else None)
    if mode in ('valid', 'replayed'):
        result = run()
        assert result.epoch == 4 and result.revision == 6 and result.replayed == (mode == 'replayed')
    else:
        with pytest.raises((ValueError, RuntimeError)): run()
    assert len(calls) <= 1 and len(closed) == len(calls)
    if mode in ('scope_false','activity_false'): assert calls == []
    assert 'Bearer private' not in repr(target)


@pytest.mark.parametrize('failure', [None, 'baseline', 'request', 'committed'])
def test_revoke_requires_independent_sql_before_and_after(monkeypatch, failure):
    f = RestoreRevokeFixture(uuid4(), uuid4(), uuid4(), 'learning_photographer', None, 3, 5, 'v1', uuid4(), uuid4())
    target = module.RestoreConsentRevokeTarget('https://restore.test', f, 'key', 'Bearer secret')
    events = []
    def step(name):
        events.append(name)
        if failure == name: raise ValueError(name)
        return name
    def observe(connection, fixture, *, revoked, **kwargs):
        assert fixture is f
        kwargs['assert_fixture_scope'](fixture)
        return step('committed' if revoked else 'baseline')
    monkeypatch.setattr(module, 'observe_fixture_learning_consent', observe)
    monkeypatch.setattr(module, 'request_fixture_consent_revoke', lambda *a, **k: step('request'))
    def run():
        return module.revoke_fixture_learning_consent(object(), target, expected_database='isolated',
            tls_context=ssl.create_default_context(), assert_fixture_scope=lambda actual: actual is target,
            check_active=lambda: None)
    if failure:
        with pytest.raises(ValueError): run()
        assert events[-1] == failure
    else:
        assert run() == ('baseline', 'request', 'committed')
    assert events == ['baseline', 'request', 'committed'][:len(events)]

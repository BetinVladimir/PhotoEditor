from contextlib import contextmanager
from dataclasses import replace
from types import SimpleNamespace
from uuid import uuid4
import ssl
import psycopg
import pytest
from photo_worker.runtime import restore_revoke_access as module
from photo_worker.runtime.restore_access_probe import RestoreAccessTarget
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode', ['baseline', 'revoked', 'foreign_database', 'unprivileged',
    'missing_lineage', 'late_lineage', 'wrong_route', 'generic_denial', 'http_failure', 'scope_failure','scope_false','activity_false'])
def test_recipe_access_requires_exact_lineage_and_rechecks_after_http(monkeypatch, mode):
    f = RestoreRevokeFixture(uuid4(), uuid4(), uuid4(), 'learning_person', uuid4(), 3, 5, 'v1', uuid4(), uuid4())
    doc = uuid4(); pin = 'a' * 64; events = []; queries = []
    access = RestoreAccessTarget('https://restore.test', f'/api/v1/tenants/{f.tenant_id}/v2-documents/{doc}',
        '/control', 'Bearer private', 409, 'PROFILE_CONSENT_REVOKED', 'b' * 64)
    if mode == 'wrong_route': access = replace(access, denied_path='/other')
    if mode == 'generic_denial': access = replace(access, expected_status=403, expected_code='FORBIDDEN')
    target = module.RestoreRevokeDocumentTarget(f, doc, pin, access)
    class Connection:
        info = SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self): yield
        def execute(self, query, params=None):
            queries.append(query); row = None
            if query.startswith('SELECT current_database'):
                row = ('foreign' if mode == 'foreign_database' else 'isolated', mode != 'unprivileged')
            if query.startswith('SELECT EXISTS'):
                assert params == (f.tenant_id, f.project_id, doc, pin, f.learning_version_id)
                row = (mode != 'missing_lineage' and not (mode == 'late_lineage' and bool(events)),)
            return SimpleNamespace(fetchone=lambda: row)
    def http(target_access, *args, **kwargs):
        assert target_access is access
        kwargs['assert_fixture_scope'](access)
        events.append('http')
        if mode == 'http_failure': raise ValueError('denial unproven')
        return 'observed'
    monkeypatch.setattr(module, 'observe_restore_access_readable', http)
    monkeypatch.setattr(module, 'observe_restore_access_denied', http)
    def scope(actual):
        assert actual is target
        if mode == 'scope_failure': raise RuntimeError('fixture lost')
        if mode == 'scope_false': return False
    def run():
        return module.observe_fixture_revoke_document(Connection(), target, expected_database='isolated',
            revoked=mode != 'baseline', tls_context=ssl.create_default_context(),
            assert_fixture_scope=scope, check_active=lambda: False if mode == 'activity_false' else None)
    if mode in ('baseline', 'revoked'):
        assert run() == 'observed'
        assert sum(q.startswith('SELECT EXISTS') for q in queries) == 2
    else:
        with pytest.raises((ValueError, RuntimeError)): run()
    if mode in ('wrong_route', 'generic_denial', 'scope_failure','scope_false','activity_false', 'missing_lineage', 'foreign_database', 'unprivileged'):
        assert not events
    if mode in ('scope_false','activity_false'):assert queries==[]
    assert not any(q.startswith(('UPDATE', 'DELETE', 'INSERT')) for q in queries)

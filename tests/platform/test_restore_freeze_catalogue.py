from contextlib import contextmanager
from types import SimpleNamespace
from uuid import uuid4
import psycopg
import pytest
from photo_worker.runtime.restore_freeze_catalogue import verify_fixture_freeze_catalogue
from photo_worker.runtime.restore_revoke_freeze import RestoreFreezeDenialTarget
from photo_worker.runtime.restore_revoke_probe import RestoreRevokeFixture


@pytest.mark.parametrize('mode', ['valid', 'foreign_db', 'unprivileged', 'stale_draft', 'replay',
    'scope', 'scope_false', 'activity_false'])
def test_freeze_current_draft_and_absent_receipt_are_independent_readonly_requirements(mode):
    f = RestoreRevokeFixture(uuid4(), uuid4(), uuid4(), 'learning_person', uuid4(), 3, 5, 'v1', uuid4(), uuid4())
    target = RestoreFreezeDenialTarget('https://restore.test', f.tenant_id, uuid4(), uuid4(), 7, 'fresh', 'Bearer private')
    version = uuid4(); queries = []
    class Connection:
        info = SimpleNamespace(transaction_status=psycopg.pq.TransactionStatus.IDLE)
        @contextmanager
        def transaction(self): yield
        def execute(self, query, params=None):
            queries.append(query); row = None
            if query.startswith('SELECT current_database'):
                row = ('foreign' if mode == 'foreign_db' else 'isolated', mode != 'unprivileged')
            elif query.startswith('SELECT EXISTS(SELECT FROM photo.assets'):
                assert params == (f.tenant_id, f.project_id, target.asset_id, version, 7, target.document_id, 'a'*64, f.learning_version_id)
                row = (mode != 'stale_draft',)
            elif query.startswith('SELECT EXISTS(SELECT FROM photo.idempotency_records'):
                assert params == (f.tenant_id, f.actor_id, 'Render.Freeze.'+str(target.asset_id), 'fresh')
                row = (mode == 'replay',)
            return SimpleNamespace(fetchone=lambda:row)
    def scope(t, fixture):
        assert t is target and fixture is f
        if mode == 'scope': raise RuntimeError('scope changed')
        if mode == 'scope_false': return False
    def run():
        return verify_fixture_freeze_catalogue(Connection(), target, f, version, 'a'*64,
            expected_database='isolated', assert_fixture_scope=scope,
            check_active=lambda:False if mode == 'activity_false' else None)
    if mode == 'valid': assert run().revision == 7
    else:
        with pytest.raises((ValueError, RuntimeError)): run()
    assert not any(q.startswith(('UPDATE', 'INSERT', 'DELETE')) for q in queries)
    if mode in ('scope_false', 'activity_false'): assert queries == []

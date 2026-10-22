"""API replay of own committed retouch Apply, without rerunning admission."""
from hashlib import sha256

import psycopg
import pytest
from retouch_copy_apply_fixture import committed_copy_fixture
from retouch_transaction_state import tenant_transaction_state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def state(db, tenant):
    return tenant_transaction_state(db, tenant)


def replay(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row = db.execute('SELECT outcome,response_body FROM photo.semantic_apply_replay('
            + ','.join(['%s']*6) + ')', args).fetchone()
        db.execute('RESET ROLE')
        return row


@pytest.mark.parametrize('with_alpha', [False, True])
def test_saved_retouch_replay_preserves_child_quota_and_receipt(database, with_alpha):
    values, task, response = committed_copy_fixture(database, with_alpha=with_alpha)
    original = values[0][0]
    request_sha = sha256(values[1].encode()).hexdigest()
    key = database.execute('SELECT key FROM photo.idempotency_records WHERE tenant_id=%s '
        "AND principal_type='user' AND principal_id=%s AND route='SemanticIntent.Apply' AND request_hash=%s",
        (original[0], original[2], request_sha)).fetchone()[0]
    args = (*original[:4], key, request_sha)
    before = state(database, original[0])
    assert replay(database, args) == ('replayed', response)
    assert replay(database, args) == ('replayed', response)
    assert replay(database, (*args[:5], 'f'*64)) == ('conflict', None)
    assert replay(database, (*args[:4], 'absent-key', args[5])) == ('absent', None)
    assert state(database, original[0]) == before
    # Saved response is durable even when new admission can no longer read
    # transient preview objects. Never start a fresh render to recover a reply.
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
            (original[0], task['params_artifact_id']))
        database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
            'WHERE tenant_id=%s AND id=%s', (original[0], task['transport']['manifest']['artifact_id']))
        before = state(database, original[0])
        assert replay(database, args) == ('replayed', response)
        assert state(database, original[0]) == before
    with database.transaction(force_rollback=True):
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (original[3],))
        before = state(database, original[0])
        with pytest.raises(psycopg.Error) as error:
            replay(database, args)
        assert error.value.sqlstate == '42501'
        assert state(database, original[0]) == before

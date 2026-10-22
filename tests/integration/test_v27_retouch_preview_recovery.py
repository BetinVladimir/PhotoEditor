"""Authoritative saved preview inspection around commit; no simulated timeout proof."""
from uuid import uuid4

import psycopg
import pytest

from retouch_preview_admission_fixture import prepared_retouch_preview_fixture, activate_retouch_preview_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('mutation', [None, 'actor', 'session', 'context', 'context_sha', 'params_sha', 'revoked_session'])
def test_saved_preview_inspection_preserves_commit_identity_and_is_read_only(database, mutation):
    data = prepared_retouch_preview_fixture(database)
    args, run = data['args'], data['run']
    context = data['scope']['retouch_context_ref']
    call = [args[0], run, args[2], args[3], args[5], args[6], context.artifact_id, context.sha256]

    def inspect(values):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
            result = database.execute('SELECT * FROM photo.inspect_semantic_retouch_preview('+','.join(['%s']*8)+')', values).fetchone()
            database.execute('RESET ROLE')
            return result

    assert inspect(call) == ('draft', '', False, False)
    task = activate_retouch_preview_fixture(database, data)
    assert task == data['task']
    assert inspect(call) == ('queued', 'ready', True, True)
    if mutation == 'actor': call[2] = uuid4()
    elif mutation == 'session': call[3] = uuid4()
    elif mutation == 'context': call[6] = uuid4()
    elif mutation == 'context_sha': call[7] = '0'*64
    elif mutation == 'params_sha': call[5] = '0'*64
    elif mutation == 'revoked_session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (args[3],))

    def snapshot():
        return (
            database.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s', (run,)).fetchone(),
            database.execute('SELECT id,state,attempt,inputs,execution_profile FROM photo.tasks WHERE run_id=%s ORDER BY id', (run,)).fetchall(),
            database.execute('SELECT artifact_id,sha256,contract,ordinal FROM photo.v2_task_inputs WHERE task_id=%s ORDER BY ordinal', (task,)).fetchall(),
            database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone(),
        )

    before = snapshot()
    if mutation is None:
        # Repeated inspection is the recovery route; no new run or activation.
        assert inspect(call) == ('queued', 'ready', True, True)
        assert inspect(call) == ('queued', 'ready', True, True)
    else:
        with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.InvalidParameterValue)):
            inspect(call)
    assert snapshot() == before


def test_preview_activation_failure_rolls_back_materialized_task_and_inputs(database):
    data = prepared_retouch_preview_fixture(database)
    # Final run update must refuse a revision that cannot be incremented safely.
    database.execute('UPDATE photo.runs SET revision=9007199254740991 WHERE id=%s', (data['run'],))
    before = database.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s', (data['run'],)).fetchone()
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (data['args'][0],)).fetchone()
    with pytest.raises(psycopg.errors.SerializationFailure):
        activate_retouch_preview_fixture(database, data)
    assert database.execute('SELECT status,graph_sealed,revision FROM photo.runs WHERE id=%s', (data['run'],)).fetchone() == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (data['run'],)).fetchone() == (0,)
    assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE task_id=%s', (data['task'],)).fetchone() == (0,)
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (data['args'][0],)).fetchone() == usage

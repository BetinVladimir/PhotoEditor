"""Durable ready-document replay must not rewrite physical version/accounting."""
from semantic_target_state_fixture import reject_target_with_revision
from uuid import uuid4

import pytest

from retouch_preview_admission_fixture import prepared_retouch_preview_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('purpose', ['task_params', 'retouch_adapter_context', 'v2_input_chunk_0', 'v2_input_manifest'])
@pytest.mark.parametrize('mutation', [None, 'token', 'version', 'bucket'])
def test_ready_document_completion_replay_is_exact_and_read_only(database, purpose, mutation):
    data = prepared_retouch_preview_fixture(database)
    args = data['args']
    ref = next(ref for name, ref, _ in data['published'] if name == purpose)
    ticket = database.execute('SELECT token,sha256,bytes,bucket,object_version FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND owner_id=%s AND purpose=%s',
                              (args[0],data['run'],data['task'],purpose)).fetchone()
    token, sha, size, bucket, version = ticket
    if mutation == 'token': token = uuid4()
    elif mutation == 'version': version += '-other'
    elif mutation == 'bucket': bucket = 'other-bucket'
    base = (args[0],args[1],args[2],args[3],data['target'],data['run'],args[5],args[6])

    def snapshot():
        return (
            database.execute('SELECT state,token,object_version,completed_at FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND owner_id=%s AND purpose=%s',
                             (args[0],data['run'],data['task'],purpose)).fetchone(),
            database.execute('SELECT state,sha256,bucket,object_key,object_version,bytes FROM photo.artifacts WHERE id=%s', (ref['artifact_id'],)).fetchone(),
            database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone(),
        )

    before = snapshot()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
        result = database.execute('SELECT photo.complete_semantic_retouch_preview_document('+','.join(['%s']*16)+')',
                                  (*base,purpose,data['task'],ref['artifact_id'],token,sha,size,bucket,version)).fetchone()[0]
        database.execute('RESET ROLE')
    assert result is (mutation is None)
    assert snapshot() == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (data['run'],)).fetchone() == (0,)


@pytest.mark.parametrize('purpose', ['task_params', 'retouch_adapter_context', 'v2_input_chunk_0', 'v2_input_manifest'])
@pytest.mark.parametrize('loss', ['session', 'target_revision', 'parent_cancel'])
def test_document_completion_rechecks_authority_after_object_io(database, purpose, loss):
    import psycopg
    data = prepared_retouch_preview_fixture(database)
    args = data['args']
    ref = next(ref for name, ref, _ in data['published'] if name == purpose)
    ticket = database.execute('SELECT token,sha256,bytes,bucket,object_version FROM photo.backend_documents WHERE tenant_id=%s AND run_id=%s AND owner_id=%s AND purpose=%s',
                              (args[0],data['run'],data['task'],purpose)).fetchone()
    if loss == 'session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (args[3],))
    elif loss == 'target_revision':
        reject_target_with_revision(database, data['target'])
    else:
        database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (data['parent'],))
    before = database.execute('SELECT state,token,object_version,completed_at FROM photo.backend_documents WHERE artifact_id=%s', (ref['artifact_id'],)).fetchone()
    usage = database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone()
    base = (args[0],args[1],args[2],args[3],data['target'],data['run'],args[5],args[6])
    with pytest.raises((psycopg.errors.InsufficientPrivilege, psycopg.errors.SerializationFailure)):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
            database.execute('SELECT photo.complete_semantic_retouch_preview_document('+','.join(['%s']*16)+')',
                             (*base,purpose,data['task'],ref['artifact_id'],*ticket)).fetchone()
    assert database.execute('SELECT state,token,object_version,completed_at FROM photo.backend_documents WHERE artifact_id=%s', (ref['artifact_id'],)).fetchone() == before
    assert database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone() == usage
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (data['run'],)).fetchone() == (0,)

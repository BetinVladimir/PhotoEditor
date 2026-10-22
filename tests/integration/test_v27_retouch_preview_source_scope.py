"""Read-only preview face catalog authority; synthetic metadata, no photo QA."""
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb

from retouch_preview_admission_fixture import prepared_retouch_preview_fixture
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('mutation', [
    None, 'tenant', 'intent', 'target', 'actor', 'session', 'revision',
    'target_revision', 'plan', 'candidate', 'source_version', 'revoked_session',
])
def test_face_catalog_requires_current_exact_preview_scope_without_writes(database, mutation):
    data = prepared_retouch_preview_fixture(database)
    params = data['scope']['retouch_params'].model_dump(mode='json')
    args = data['args']
    source = dict(params['source'])
    call = [args[0], args[1], data['target'], args[2], args[3],
            params['workflow_revision'], params['target_revision'],
            params['plan']['sha256'], params['candidate']['sha256'], source]
    for index, name in enumerate(('tenant', 'intent', 'target', 'actor', 'session')):
        if mutation == name:
            call[index] = uuid4()
    if mutation == 'revision':
        call[5] = int(call[5]) + 1
    if mutation == 'target_revision':
        call[6] = int(call[6]) + 1
    if mutation == 'plan':
        call[7] = '0' * 64
    if mutation == 'candidate':
        call[8] = '0' * 64
    if mutation == 'source_version':
        source['asset_version_id'] = str(uuid4())
    if mutation == 'revoked_session':
        database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (args[3],))

    def snapshot():
        return (
            database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (data['run'],)).fetchone(),
            database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s', (data['target'],)).fetchone(),
            database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (args[0],)).fetchone(),
        )

    before = snapshot()
    # A savepoint recovers the expected SQL refusal without hiding later checks.
    if mutation is not None:
        with pytest.raises(psycopg.errors.InsufficientPrivilege):
            with database.transaction():
                database.execute('SET LOCAL ROLE photo_api')
                database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
                database.execute('SELECT * FROM photo.semantic_retouch_preview_landmark_catalog(' + ','.join(['%s'] * 10) + ')',
                                 (*call[:9], Jsonb(source))).fetchall()
    else:
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(args[0]),))
            rows = database.execute('SELECT * FROM photo.semantic_retouch_preview_landmark_catalog(' + ','.join(['%s'] * 10) + ')',
                                    (*call[:9], Jsonb(source))).fetchall()
            database.execute('RESET ROLE')
        expected = data['scope']['retouch_context'].model_dump(mode='json')['faces'][0]['landmarks']
        assert len(rows) == 1
        assert str(rows[0][0]) == expected['artifact_id']
        assert rows[0][1] == expected['sha256']
    assert snapshot() == before


@pytest.mark.parametrize('kind', ['source', 'reference', 'mask', 'transform', 'protection'])
def test_preapply_source_inventory_is_exact_and_read_only(database, kind):
    import json
    data = prepared_retouch_preview_fixture(database)
    scope = data['scope']
    params = scope['retouch_params'].model_dump(mode='json')
    candidate = json.loads(scope['candidate_body'])
    intent = json.loads(scope['intent_body'])
    base = (*data['args'][:4], int(params['workflow_revision']), data['target'],
            int(params['target_revision']), params['source']['asset_version_id'],
            params['candidate']['artifact_id'], params['candidate']['sha256'])
    if kind in ('source', 'reference'):
        ref = params['source']['source'] if kind == 'source' else intent['reference']['source']
        name = 'semantic_approved_image_object'
        suffix = (kind == 'reference', ref['artifact_id'], ref['sha256'])
        expected = 'WorkingImage'
    elif kind in ('mask', 'transform'):
        ref = candidate[kind]
        name = 'semantic_approved_candidate_child'
        suffix = (scope['candidate_body'].decode(), kind)
        expected = 'MaskRef' if kind == 'mask' else 'ResolvedTransform'
    else:
        ref = params['protection']
        name = 'semantic_approved_protection_object'
        suffix = (ref['artifact_id'], ref['sha256'])
        expected = 'MaskRef'
    call = (*base, *suffix)
    query = 'SELECT * FROM photo.' + name + '(' + ','.join(['%s'] * len(call)) + ')'
    before = database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s', (data['target'],)).fetchone()
    with database.transaction():
        database.execute('SET LOCAL ROLE photo_api')
        database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(data['args'][0]),))
        row = database.execute(query, call).fetchone()
        database.execute('RESET ROLE')
    assert row is not None and row[0] == expected
    assert str(row[1]) == ref['artifact_id'] and row[2] == ref['sha256']
    assert row[4] and row[5] and row[6] not in ('', 'null', None)
    assert row[7] == 'application/json'
    # A ready pin does not survive withdrawal of verified physical metadata.
    database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (ref['artifact_id'],))
    if kind in ('source','reference'):
        # Full review-source authority is checked before object selection;
        # withdrawn physical verification is a declared stale-publication error.
        with pytest.raises(psycopg.errors.SerializationFailure) as refusal:
            with database.transaction():
                database.execute('SET LOCAL ROLE photo_api')
                database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(data['args'][0]),))
                database.execute(query, call).fetchone()
        assert refusal.value.sqlstate == '40001'
        assert refusal.value.diag.message_primary == 'SEMANTIC_PUBLICATION_STALE'
    else:
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_api')
            database.execute("SELECT set_config('app.tenant_id',%s,true)", (str(data['args'][0]),))
            assert database.execute(query, call).fetchone() is None
            database.execute('RESET ROLE')
    assert database.execute('SELECT state,revision,child_run_id,batch_id FROM photo.semantic_targets WHERE id=%s', (data['target'],)).fetchone() == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE run_id=%s', (data['run'],)).fetchone() == (0,)

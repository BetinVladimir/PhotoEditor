"""Own pre-child Params publication/replay; storage versions are metadata-only."""
from uuid import uuid4, UUID
import json
import pytest
import psycopg
from retouch_copy_apply_fixture import shown_copy_params_fixture, shown_copy_transport_fixture
from photo_worker.contracts.base import canonical_bytes
from photo_worker.contracts.semantic_preview import ShownRetouchAdoptionInput
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def api(db, function, args):
    assert function in ('reserve_semantic_shown_retouch_transport_document',
        'complete_semantic_shown_retouch_transport_document')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row = db.execute('SELECT * FROM photo.'+function+'('+','.join(['%s']*len(args))+')', args).fetchone()
        db.execute('RESET ROLE')
        return row


@pytest.mark.parametrize('with_alpha', [False, True])
def test_retouch_params_publication_replays_without_child_or_extra_quota(database, with_alpha):
    values, params, task, artifact, raw, sha, base = shown_copy_params_fixture(database, with_alpha=with_alpha)
    tenant = base[0]
    reserve = (*base, 'task_params', task, artifact, sha, len(raw.encode()), 'fixture')
    first = api(database, 'reserve_semantic_shown_retouch_transport_document', reserve)
    assert first[0] == 'created' and first[1] == artifact and first[3] is False
    complete = (*base, 'task_params', task, artifact, first[2], sha, len(raw.encode()), 'fixture', 'metadata-only-v1')
    # No task or child is created by publication; admission remains independent.
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE id=%s', (task,)).fetchone() == (0,)

    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s', (base[11],)).fetchone() == (0,)
    assert api(database, 'complete_semantic_shown_retouch_transport_document', complete) == (True,)
    def snapshot():
        return (
            database.execute('SELECT to_jsonb(d) FROM photo.backend_documents d WHERE tenant_id=%s AND owner_id=%s', (tenant, task)).fetchall(),
            database.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone(),
            database.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE tenant_id=%s AND id=%s', (tenant, artifact)).fetchone(),
        )
    before = snapshot()
    for _ in range(2):
        assert api(database, 'reserve_semantic_shown_retouch_transport_document', reserve) == ('replayed', artifact, first[2], True)
        assert api(database, 'complete_semantic_shown_retouch_transport_document', complete) == (True,)
        assert snapshot() == before
    for index, replacement in ((15, uuid4()), (16, 'e'*64), (17, len(raw.encode())+1), (18, 'other'), (19, 'different-version')):
        changed = list(complete);changed[index] = replacement
        if index in (16, 17):
            with pytest.raises(psycopg.Error) as error:
                api(database, 'complete_semantic_shown_retouch_transport_document', tuple(changed))
            assert error.value.sqlstate == '22023'
        else:
            assert api(database, 'complete_semantic_shown_retouch_transport_document', tuple(changed)) == (False,)
        assert snapshot() == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE id=%s', (task,)).fetchone() == (0,)


def publication_snapshot(db, tenant, task):
    return (
        db.execute('SELECT to_jsonb(d) FROM photo.backend_documents d WHERE tenant_id=%s AND owner_id=%s ORDER BY id', (tenant, task)).fetchall(),
        db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone(),
        db.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE tenant_id=%s AND id IN '
            '(SELECT artifact_id FROM photo.backend_documents WHERE tenant_id=%s AND owner_id=%s) ORDER BY id',
            (tenant, tenant, task)).fetchall(),
    )


def test_retouch_transport_ready_replays_preserve_all_documents(database):
    values, copy, node, transport, inventory = shown_copy_transport_fixture(database, with_alpha=True)
    args = values[0][0]; preview = json.loads(args[5])
    raw = canonical_bytes(ShownRetouchAdoptionInput.model_validate_json(canonical_bytes(copy))).decode()
    base = (*args[:4], int(preview['workflow_revision']), args[4], int(preview['target_revision']),
        preview['plan']['sha256'], preview['candidate']['sha256'], raw, node['params_sha'], UUID(copy['child_run_id']))
    task = UUID(node['id']); tenant = args[0]
    docs = database.execute('SELECT purpose,artifact_id,token,sha256,bytes,bucket,object_version '
        'FROM photo.backend_documents WHERE tenant_id=%s AND owner_id=%s ORDER BY purpose', (tenant, task)).fetchall()
    assert {d[0] for d in docs} == {'task_params', 'v2_input_chunk_0', 'v2_input_manifest'}
    before = publication_snapshot(database, tenant, task)
    for purpose, artifact, token, sha, size, bucket, version in docs:
        reserve = (*base, purpose, task, artifact, sha, size, bucket)
        complete = (*base, purpose, task, artifact, token, sha, size, bucket, version)
        assert api(database, 'reserve_semantic_shown_retouch_transport_document', reserve) == ('replayed', artifact, token, True)
        assert api(database, 'complete_semantic_shown_retouch_transport_document', complete) == (True,)
        assert publication_snapshot(database, tenant, task) == before
        if purpose != 'task_params':
            changed = list(reserve); changed[15] = 'e'*64
            assert api(database, 'reserve_semantic_shown_retouch_transport_document', tuple(changed))[0] == 'conflict'
            assert publication_snapshot(database, tenant, task) == before
    assert database.execute('SELECT count(*) FROM photo.tasks WHERE id=%s', (task,)).fetchone() == (0,)


@pytest.mark.parametrize('mutation', ['session', 'preview_cancel', 'producer_cancel', 'approved_unverified'])
def test_retouch_publication_reauthorizes_before_completion(database, mutation):
    values, params, task, artifact, raw, sha, base = shown_copy_params_fixture(database)
    reserve = (*base, 'task_params', task, artifact, sha, len(raw.encode()), 'fixture')
    ticket = api(database, 'reserve_semantic_shown_retouch_transport_document', reserve)
    assert ticket[0] == 'created'
    complete = (*base, 'task_params', task, artifact, ticket[2], sha, len(raw.encode()), 'fixture', 'metadata-only-v1')
    with database.transaction(force_rollback=True):
        if mutation == 'session':
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (base[3],))
        elif mutation == 'preview_cancel':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s', (values[0][1],))
        elif mutation == 'producer_cancel':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s', (values[0][7],))
        else:
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (base[0], params['approved_edit']['artifact_id']))
        before = publication_snapshot(database, base[0], task)
        with pytest.raises(psycopg.Error) as error:
            api(database, 'complete_semantic_shown_retouch_transport_document', complete)
        assert error.value.sqlstate == ('42501' if mutation == 'session' else '40001')
        assert publication_snapshot(database, base[0], task) == before
    assert api(database, 'complete_semantic_shown_retouch_transport_document', complete) == (True,)

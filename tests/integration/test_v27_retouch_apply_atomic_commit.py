"""Whole own Apply SQL transaction and late event-cursor rollback scenarios."""
from semantic_target_state_fixture import reject_target_with_revision
from hashlib import sha256
import json
from uuid import UUID, uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb
from retouch_copy_apply_fixture import admission_task_fixture
from test_v27_retouch_apply_saved_replay import replay
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def snapshot(db, tenant):
    from retouch_transaction_state import tenant_transaction_state
    return tenant_transaction_state(db, tenant)


def pending(db, *, with_alpha=False):
    values, task = admission_task_fixture(db, with_alpha=with_alpha)
    data, request, artifact, body, child = values[:5]
    args = data[0]
    revision = int(json.loads(args[5])['workflow_revision'])
    key = str(uuid4())
    request_sha = sha256(request.encode()).hexdigest()
    db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
        "VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
        (args[0], args[2], key, request_sha))
    return values, task, (*args[:4], key, revision, request, artifact, body,
        Jsonb(data[2]['review_documents']), child, Jsonb([task]))


def commit(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_semantic_admission')
        result = db.execute('SELECT photo.commit_semantic_shown_retouch_apply_review('
            + ','.join(['%s']*12) + ')', args).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('with_alpha', [False, True])
def test_atomic_apply_commits_child_inputs_quota_receipt_and_events_once(database, with_alpha):
    values, task, args = pending(database, with_alpha=with_alpha)
    tenant, child = args[0], values[4]
    event_before = database.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    outbox_before = database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    result = commit(database, args)
    assert result['child'] == dict(run_id=str(child), state='queued')
    assert result['revision'] == str(args[5] + 1) and result['remaining'] == 0
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',
        (tenant, child)).fetchone() == ('queued', True)
    assert database.execute('SELECT state,attempt FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',
        (tenant, child)).fetchall() == [('ready', 0)]
    assert database.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s',
        (tenant, task['node']['id'])).fetchone()[0] == len(task['inventory']['entries']) + 2
    assert database.execute('SELECT response_status,response_body,resource_id FROM photo.idempotency_records '
        "WHERE tenant_id=%s AND route='SemanticIntent.Apply' AND key=%s", (tenant, args[4])).fetchone() == (200, result, UUID(result['batch_id']))
    assert database.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s', (tenant,)).fetchone()[0] == event_before + 1
    assert database.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0] == outbox_before + 1
    before = snapshot(database, tenant)
    replay_args = (*args[:4], args[4], sha256(args[6].encode()).hexdigest())
    assert replay(database, replay_args) == ('replayed', result)
    with pytest.raises(psycopg.Error) as error:
        commit(database, args)
    assert error.value.sqlstate == '40001'
    assert snapshot(database, tenant) == before


@pytest.mark.parametrize('with_alpha', [False, True])
def test_late_event_cursor_exhaustion_rolls_back_entire_apply(database, with_alpha):
    values, _, args = pending(database, with_alpha=with_alpha)
    tenant, child = args[0], values[4]
    with database.transaction(force_rollback=True):
        database.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) '
            'VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor', (tenant,))
        before = snapshot(database, tenant)
        with pytest.raises(psycopg.Error) as error:
            commit(database, args)
        assert error.value.sqlstate == '23514'
        assert 'EVENT_CURSOR_EXHAUSTED' in str(error.value)
        assert snapshot(database, tenant) == before
        assert database.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == (0,)
        assert database.execute('SELECT response_status FROM photo.idempotency_records '
            "WHERE tenant_id=%s AND route='SemanticIntent.Apply' AND key=%s", (tenant, args[4])).fetchone() == (None,)
    # The same pending request still succeeds once the exhausted cursor rolls back.
    assert commit(database, args)['child']['run_id'] == str(child)


@pytest.mark.parametrize('with_alpha',[False,True])
def test_successful_apply_is_fully_removed_by_caller_rollback_and_can_retry(database,with_alpha):
    values,_,args=pending(database,with_alpha=with_alpha)
    before=snapshot(database,args[0])
    with database.transaction(force_rollback=True):
        response=commit(database,args)
        assert response['child']['run_id']==str(values[4])
        assert snapshot(database,args[0])!=before
    assert snapshot(database,args[0])==before
    # Same request, child and idempotency key remain usable after caller rollback.
    response=commit(database,args)
    assert response['child']['run_id']==str(values[4])
    assert replay(database,(*args[:5],sha256(args[6].encode()).hexdigest()))==('replayed',response)


@pytest.mark.parametrize('mutation',[
    'session','target','preview','producer','working_image','before','receipt',
])
def test_full_apply_refuses_late_selection_loss_without_partial_persistence(database,mutation):
    values,_,args=pending(database,with_alpha=True)
    data=values[0]
    with database.transaction(force_rollback=True):
        if mutation=='session':
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
        elif mutation=='target':
            reject_target_with_revision(database, data[0][4])
        elif mutation=='preview':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(data[1],))
        elif mutation=='producer':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(data[7],))
        elif mutation=='receipt':
            database.execute('UPDATE photo.runs SET v27_retouch_preview_receipt=NULL WHERE id=%s',(data[1],))
        else:
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s '
                'AND created_by_task_id=%s AND output_role=%s',(args[0],data[7],mutation))
        before=snapshot(database,args[0])
        with pytest.raises(psycopg.Error) as failure:
            commit(database,args)
        assert failure.value.sqlstate in ('40001','42501','23514')
        assert snapshot(database,args[0])==before
        assert database.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s',
            (args[0],values[4])).fetchone()==(0,)
    assert commit(database,args)['child']['run_id']==str(values[4])

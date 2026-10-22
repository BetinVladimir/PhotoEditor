"""Whole own Apply SQL transaction and late event-cursor rollback scenarios."""
from semantic_target_state_fixture import reject_target_with_revision
from hashlib import sha256
import json
from uuid import UUID, uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb
from removal_apply_documents_fixture import admission_task_fixture


def replay(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_api')
        row = db.execute('SELECT outcome,response_body FROM photo.semantic_apply_replay('
            + ','.join(['%s'] * 6) + ')', args).fetchone()
        db.execute('RESET ROLE')
        return row


def snapshot(db, tenant):
    from removal_transaction_state import tenant_transaction_state
    return tenant_transaction_state(db, tenant)


def pending(db):
    values, task = admission_task_fixture(db)
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
        result = db.execute('SELECT photo.commit_semantic_shown_removal_apply_review('
            + ','.join(['%s']*12) + ')', args).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def test_atomic_apply_commits_child_inputs_quota_receipt_and_events_once(db):
    values, task, args = pending(db)
    tenant, child = args[0], values[4]
    event_before = db.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    outbox_before = db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    result = commit(db, args)
    assert result['child'] == dict(run_id=str(child), state='queued')
    assert result['revision'] == str(args[5] + 1) and result['remaining'] == 0
    assert db.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',
        (tenant, child)).fetchone() == ('queued', True)
    assert db.execute('SELECT state,attempt FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',
        (tenant, child)).fetchall() == [('ready', 0)]
    assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s',
        (tenant, task['node']['id'])).fetchone()[0] == len(task['inventory']['entries']) + 2
    assert db.execute('SELECT response_status,response_body,resource_id FROM photo.idempotency_records '
        "WHERE tenant_id=%s AND route='SemanticIntent.Apply' AND key=%s", (tenant, args[4])).fetchone() == (200, result, UUID(result['batch_id']))
    assert db.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s', (tenant,)).fetchone()[0] == event_before + 1
    assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s', (tenant,)).fetchone()[0] == outbox_before + 1
    before = snapshot(db, tenant)
    replay_args = (*args[:4], args[4], sha256(args[6].encode()).hexdigest())
    assert replay(db, replay_args) == ('replayed', result)
    with pytest.raises(psycopg.Error) as error:
        commit(db, args)
    assert error.value.sqlstate == '40001'
    assert snapshot(db, tenant) == before


def test_late_event_cursor_exhaustion_rolls_back_entire_apply(db):
    values, _, args = pending(db)
    tenant, child = args[0], values[4]
    with db.transaction(force_rollback=True):
        db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) '
            'VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor', (tenant,))
        before = snapshot(db, tenant)
        with pytest.raises(psycopg.Error) as error:
            commit(db, args)
        assert error.value.sqlstate == '23514'
        assert 'EVENT_CURSOR_EXHAUSTED' in str(error.value)
        assert snapshot(db, tenant) == before
        assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s', (tenant, child)).fetchone() == (0,)
        assert db.execute('SELECT response_status FROM photo.idempotency_records '
            "WHERE tenant_id=%s AND route='SemanticIntent.Apply' AND key=%s", (tenant, args[4])).fetchone() == (None,)
    # The same pending request still succeeds once the exhausted cursor rolls back.
    assert commit(db, args)['child']['run_id'] == str(child)


def test_successful_apply_is_fully_removed_by_caller_rollback_and_can_retry(db):
    values,_,args=pending(db)
    before=snapshot(db,args[0])
    with db.transaction(force_rollback=True):
        response=commit(db,args)
        assert response['child']['run_id']==str(values[4])
        assert snapshot(db,args[0])!=before
    assert snapshot(db,args[0])==before
    # Same request, child and idempotency key remain usable after caller rollback.
    response=commit(db,args)
    assert response['child']['run_id']==str(values[4])
    assert replay(db,(*args[:5],sha256(args[6].encode()).hexdigest()))==('replayed',response)


@pytest.mark.parametrize('mutation',[
    'session','target','preview','producer','working_image','before','receipt',
])
def test_full_apply_refuses_late_selection_loss_without_partial_persistence(db,mutation):
    values,_,args=pending(db)
    data=values[0]
    with db.transaction(force_rollback=True):
        if mutation=='session':
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s',(args[3],))
        elif mutation=='target':
            reject_target_with_revision(db, data[0][4])
        elif mutation=='preview':
            db.execute('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',(data[1],))
        elif mutation=='producer':
            db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(data[7],))
        elif mutation=='receipt':
            db.execute('UPDATE photo.runs SET v27_removal_preview_receipt=NULL WHERE id=%s',(data[1],))
        else:
            db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s '
                'AND created_by_task_id=%s AND output_role=%s',(args[0],data[7],mutation))
        before=snapshot(db,args[0])
        with pytest.raises(psycopg.Error) as failure:
            commit(db,args)
        assert failure.value.sqlstate in ('40001','42501','23514')
        assert snapshot(db,args[0])==before
        assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND id=%s',
            (args[0],values[4])).fetchone()==(0,)
    assert commit(db,args)['child']['run_id']==str(values[4])


@pytest.mark.parametrize('mode', ['valid', 'cursor_exhausted', 'caller_rollback'])
def test_removal_atomic_multi_target_apply_is_all_or_nothing(db, mode):
    from removal_multi_selection_fixture import multi_apply_task_fixture
    bundles, gate, child, tasks = multi_apply_task_fixture(db)
    key = str(uuid4())
    db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
        "VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
        (gate[0], gate[2], key, sha256(gate[5].encode()).hexdigest()))
    args = (*gate[:4], key, *gate[4:8], Jsonb(gate[8]), child, Jsonb(tasks))
    if mode == 'cursor_exhausted':
        db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) '
            'ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor', (gate[0],))
    before = snapshot(db, gate[0])
    if mode == 'cursor_exhausted':
        with pytest.raises(psycopg.Error) as failure:
            commit(db, args)
        assert failure.value.sqlstate == '23514' and 'EVENT_CURSOR_EXHAUSTED' in str(failure.value)
        assert snapshot(db, gate[0]) == before
        return
    if mode == 'caller_rollback':
        with db.transaction(force_rollback=True):
            assert commit(db, args)['child']['run_id'] == str(child)
        assert snapshot(db, gate[0]) == before
    result = commit(db, args)
    assert result['remaining'] == 0 and result['child'] == dict(run_id=str(child), state='queued')
    assert db.execute('SELECT count(*) FROM photo.tasks WHERE tenant_id=%s AND run_id=%s AND state=\'ready\'', (gate[0], child)).fetchone() == (2,)
    for task in tasks:
        assert db.execute('SELECT count(*) FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s', (gate[0], task['node']['id'])).fetchone()[0] == len(task['inventory']['entries']) + 2
    for bundle in bundles:
        assert db.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s', (gate[0], bundle['target'])).fetchone() == ('queued', child, UUID(result['batch_id']))
    committed = snapshot(db, gate[0])
    assert replay(db, (*gate[:4], key, sha256(gate[5].encode()).hexdigest())) == ('replayed', result)
    assert snapshot(db, gate[0]) == committed


@pytest.mark.parametrize('mode', ['profile', 'entry_count', 'entries_sha', 'input_digest', 'manifest'])
def test_removal_sealed_input_gate_refuses_changed_expected_inventory(db, mode):
    from copy import deepcopy
    from test_v27_removal_apply_reserved_draft import prepared, reserve
    values, task, draft_args = prepared(db)
    reserve(db, draft_args)
    tenant, project, actor, child = draft_args[0], values[0][2]['project'], draft_args[2], values[4]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT photo.seal_semantic_shown_removal_apply_inputs(%s,%s,%s,%s,%s)',
            (tenant, project, actor, child, Jsonb([task]))).fetchone() == (1,)
        assert db.execute('SELECT photo.semantic_shown_removal_apply_sealed_inputs_ready(%s,%s,%s,%s,%s)',
            (tenant, project, actor, child, Jsonb([task]))).fetchone() == (True,)
        db.execute('RESET ROLE')
    changed = deepcopy(task)
    if mode == 'profile':
        changed['node']['execution_profile']['dataset_sha'] = '0' * 64
    elif mode in ('entry_count', 'entries_sha'):
        changed['transport'][mode] = 1 if mode == 'entry_count' else '0' * 64
    elif mode == 'input_digest':
        changed['node']['input_digest'] = '0' * 64
    else:
        changed['transport']['manifest']['artifact_id'] = str(uuid4())
    before = snapshot(db, tenant)
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        assert db.execute('SELECT photo.semantic_shown_removal_apply_sealed_inputs_ready(%s,%s,%s,%s,%s)',
            (tenant, project, actor, child, Jsonb([changed]))).fetchone() == (False,)
        db.execute('RESET ROLE')
    assert snapshot(db, tenant) == before

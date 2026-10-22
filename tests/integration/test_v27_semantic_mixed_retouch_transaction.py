"""Own retouch mixed review SQL scenarios; synthetic report/object metadata only."""
from copy import deepcopy
from uuid import UUID, uuid4
import json
import psycopg
import pytest
from semantic_mixed_retouch_fixture import mixed_decision_fixture, commit_mixed_decision
from semantic_rejection_fixture import replay_rejection
from removal_transaction_state import tenant_transaction_state
from semantic_target_state_fixture import reject_target_with_revision


@pytest.mark.parametrize('approved_count', [1, 2])
@pytest.mark.parametrize('reverse', [False, True])
def test_mixed_decisions_admit_only_approved_and_preserve_omitted(db, approved_count, reverse):
    f = mixed_decision_fixture(db, approved_count=approved_count, reverse=reverse)
    tenant, intent = f['args'][:2]
    omitted = db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t WHERE tenant_id=%s AND id=ANY(%s) ORDER BY id',
        (tenant, f['omitted'])).fetchall()
    # Rejected/omitted targets have no preview, not a fabricated successful one.
    assert db.execute("SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND "
        "config_snapshot->'semantic_retouch_preview'->'params'->>'target_id'=ANY(%s)",
        (tenant, [str(target) for target in f['rejected']+f['omitted']])).fetchone() == (0,)
    result = commit_mixed_decision(db, f)
    batch = UUID(result['batch_id'])
    assert result['remaining'] == 1 and result['revision'] == str(f['revision']+1)
    assert result['child'] == dict(run_id=str(f['child']), state='queued')
    for ids, state, child in ((f['approved'], 'queued', f['child']), (f['rejected'], 'rejected', None)):
        assert db.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets WHERE tenant_id=%s AND id=ANY(%s) ORDER BY id',
            (tenant, ids)).fetchall() == [(state, child, batch)]*len(ids)
    assert db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t WHERE tenant_id=%s AND id=ANY(%s) ORDER BY id',
        (tenant, f['omitted'])).fetchall() == omitted
    expected_tasks = sorted(UUID(task['node']['id']) for task in f['tasks'])
    assert db.execute('SELECT id,state,operation FROM photo.tasks WHERE tenant_id=%s AND run_id=%s ORDER BY id',
        (tenant, f['child'])).fetchall() == [(task, 'ready', 'semantic.adopt_shown_retouch_region') for task in expected_tasks]
    total = sum(task['node']['execution_profile']['resource_estimate']['output_bytes'] for task in f['tasks'])
    assert db.execute("SELECT bytes,files,state FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s",
        (tenant, f['child'])).fetchall() == [(total, 0, 'reserved')]
    before = tenant_transaction_state(db, tenant)
    assert replay_rejection(db, f) == ('replayed', result)
    assert tenant_transaction_state(db, tenant) == before
    assert replay_rejection(db, f, digest='0'*64) == ('conflict', None)
    with pytest.raises(psycopg.Error):
        commit_mixed_decision(db, f, key=str(uuid4()))
    assert tenant_transaction_state(db, tenant) == before


@pytest.mark.parametrize('mutation', ['missing_approved', 'duplicate_task', 'task_for_rejected',
    'task_for_omitted', 'wrong_shown_image', 'legacy_operation', 'second_profile'])
def test_mixed_decision_task_inventory_refusal_is_atomic(db, mutation):
    f = mixed_decision_fixture(db, approved_count=2)
    tasks = deepcopy(f['tasks'])
    if mutation == 'missing_approved': tasks.pop()
    elif mutation == 'duplicate_task': tasks.append(deepcopy(tasks[0]))
    elif mutation in ('task_for_rejected', 'task_for_omitted'):
        tasks[1]['target_id'] = str(f['rejected' if mutation == 'task_for_rejected' else 'omitted'][0])
    elif mutation == 'wrong_shown_image':
        body = json.loads(tasks[1]['params_body'])
        body['working_image'] = json.loads(tasks[0]['params_body'])['working_image']
        from hashlib import sha256
        from photo_worker.contracts.base import canonical_bytes
        tasks[1]['params_body'] = canonical_bytes(body).decode()
        tasks[1]['params_sha'] = sha256(tasks[1]['params_body'].encode()).hexdigest()
        tasks[1]['node']['params_sha'] = tasks[1]['params_sha']
    elif mutation == 'legacy_operation': tasks[1]['node']['operation'] = 'semantic.local_exposure'
    else: tasks[1]['node']['execution_profile']['resource_estimate']['output_bytes'] = 1
    before = tenant_transaction_state(db, f['args'][0])
    with pytest.raises(psycopg.Error):
        commit_mixed_decision(db, f, tasks=tasks)
    assert tenant_transaction_state(db, f['args'][0]) == before
    assert commit_mixed_decision(db, f)['remaining'] == 1


@pytest.mark.parametrize('mutation', ['rejected_revision', 'rejected_candidate', 'session'])
def test_mixed_decision_rechecks_rejected_target_authority_before_any_activation(db, mutation):
    f = mixed_decision_fixture(db)
    tenant = f['args'][0]
    with db.transaction(force_rollback=True):
        if mutation == 'rejected_revision':
            # A bare revision update is not a legal lifecycle transition.
            # Keep its exact immutable refusal, then exercise stale authority
            # through a lawful proposed→rejected transition (revision 1→2).
            immutable_before = tenant_transaction_state(db, tenant)
            with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState) as immutable:
                with db.transaction():
                    db.execute('UPDATE photo.semantic_targets SET revision=revision+1 WHERE tenant_id=%s AND id=%s', (tenant, f['rejected'][0]))
            assert immutable.value.sqlstate == '55000'
            assert immutable.value.diag.message_primary == 'SEMANTIC_TARGET_STATE_CONFLICT'
            assert tenant_transaction_state(db, tenant) == immutable_before
            prior = db.execute('SELECT state,revision FROM photo.semantic_targets WHERE tenant_id=%s AND id=%s',
                (tenant, f['rejected'][0])).fetchone()
            assert prior == ('proposed', 1)
            advanced = reject_target_with_revision(db, f['rejected'][0])
            assert advanced[:2] == ('rejected', prior[1]+1)
        elif mutation == 'rejected_candidate':
            aid = json.loads(f['reviews'][1][0][5])['candidate']['artifact_id']
            db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s', (tenant, aid))
        else:
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (f['args'][3],))
        before = tenant_transaction_state(db, tenant)
        if mutation == 'rejected_revision':
            # The original published mixed decision still names revision1.
            # Full mixed commit must recheck even targets that it would reject.
            with pytest.raises(psycopg.errors.SerializationFailure) as stale:
                commit_mixed_decision(db, f)
            assert stale.value.sqlstate == '40001'
            assert stale.value.diag.message_primary == 'SEMANTIC_DECISION_TARGET_CONFLICT'
        else:
            with pytest.raises(psycopg.Error): commit_mixed_decision(db, f)
        assert tenant_transaction_state(db, tenant) == before
    assert commit_mixed_decision(db, f)['remaining'] == 1


def test_mixed_decision_late_failure_rolls_back_both_acceptance_and_rejection(db):
    f = mixed_decision_fixture(db, approved_count=2)
    tenant = f['args'][0]
    with db.transaction(force_rollback=True):
        db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) '
            'ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor', (tenant,))
        before = tenant_transaction_state(db, tenant)
        with pytest.raises(psycopg.errors.CheckViolation, match='EVENT_CURSOR_EXHAUSTED'):
            commit_mixed_decision(db, f)
        assert tenant_transaction_state(db, tenant) == before
    assert commit_mixed_decision(db, f)['remaining'] == 1


def test_mixed_decision_caller_rollback_and_retry(db):
    f = mixed_decision_fixture(db)
    before = tenant_transaction_state(db, f['args'][0])
    with db.transaction(force_rollback=True):
        assert commit_mixed_decision(db, f)['remaining'] == 1
    assert tenant_transaction_state(db, f['args'][0]) == before
    assert commit_mixed_decision(db, f)['remaining'] == 1


@pytest.mark.parametrize('mutation', ['storage_limit', 'missing_usage', 'disabled_copy_policy', 'unverified_copy_report'])
def test_mixed_retouch_late_quota_and_qualification_refusal_rolls_back_all_targets(db, mutation):
    f = mixed_decision_fixture(db, approved_count=2)
    tenant = f['args'][0]
    pin = f['tasks'][0]['node']['execution_profile']['qualification']
    with db.transaction(force_rollback=True):
        if mutation == 'storage_limit':
            db.execute("UPDATE photo.tenants SET quotas=jsonb_set(quotas,'{storage_bytes}','0'::jsonb) WHERE id=%s", (tenant,))
        elif mutation == 'missing_usage':
            db.execute('DELETE FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,))
        elif mutation == 'disabled_copy_policy':
            db.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s', (pin['policy_id'],))
        else:
            db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s', (tenant, pin['report']['artifact_id']))
        before = tenant_transaction_state(db, tenant)
        with pytest.raises(psycopg.Error):
            commit_mixed_decision(db, f)
        assert tenant_transaction_state(db, tenant) == before
    assert commit_mixed_decision(db, f)['remaining'] == 1



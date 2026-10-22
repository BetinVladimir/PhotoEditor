"""Reject-only SQL transaction scenarios, authored but not execution evidence.

Run against the complete migration chain, both a fresh db and an upgrade
from 02410 to 02411. The fixture never substitutes SQL authority functions.
"""
from semantic_target_state_fixture import reject_target_with_revision
import json
from uuid import uuid4
import psycopg
import pytest
from semantic_rejection_fixture import rejection_fixture, commit_rejection, replay_rejection
from removal_transaction_state import tenant_transaction_state


EXECUTION_TABLES = {'runs', 'tasks', 'task_results', 'task_attempts', 'quota_reservations',
    'tenant_usage', 'v2_task_inputs', 'output_uploads', 'asset_versions', 'asset_version_sources'}


def execution_state(db, tenant):
    return tuple(row for row in tenant_transaction_state(db, tenant) if row[0] in EXECUTION_TABLES - {'runs'})


@pytest.mark.parametrize('selected_count', [1, 2])
def test_rejection_commits_only_explicit_targets_without_execution_and_replays(db, selected_count):
    f = rejection_fixture(db, selected_count=selected_count)
    tenant, intent = f['args'][:2]
    before = execution_state(db, tenant)
    runs = db.execute('SELECT id FROM photo.runs WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall()
    omitted = db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t '
        'WHERE tenant_id=%s AND intent_id=%s AND NOT(id=ANY(%s)) ORDER BY id',
        (tenant, intent, f['selected'])).fetchall()
    result = commit_rejection(db, f)
    assert result['child'] is None and result['remaining'] == 2-selected_count
    assert result['revision'] == str(f['revision']+1)
    assert execution_state(db, tenant) == before
    assert db.execute('SELECT id FROM photo.runs WHERE tenant_id=%s ORDER BY id', (tenant,)).fetchall() == runs
    assert db.execute('SELECT status,revision FROM photo.runs WHERE tenant_id=%s AND id=%s',
        (tenant, f['parent'])).fetchone() == ('waiting_review', f['revision']+1)
    assert db.execute('SELECT state,child_run_id,batch_id FROM photo.semantic_targets '
        'WHERE tenant_id=%s AND id=ANY(%s) ORDER BY id', (tenant, f['selected'])).fetchall() == [
            ('rejected', None, uuid_from(result['batch_id']))]*selected_count
    assert db.execute('SELECT to_jsonb(t) FROM photo.semantic_targets t '
        'WHERE tenant_id=%s AND intent_id=%s AND NOT(id=ANY(%s)) ORDER BY id',
        (tenant, intent, f['selected'])).fetchall() == omitted
    snapshot = tenant_transaction_state(db, tenant)
    assert replay_rejection(db, f) == ('replayed', result)
    assert tenant_transaction_state(db, tenant) == snapshot
    assert replay_rejection(db, f, digest='0'*64) == ('conflict', None)
    assert tenant_transaction_state(db, tenant) == snapshot
    with pytest.raises(psycopg.Error):
        commit_rejection(db, f, key=str(uuid4()))
    assert tenant_transaction_state(db, tenant) == snapshot


def uuid_from(value):
    from uuid import UUID
    return UUID(str(value))


@pytest.mark.parametrize('mutation', ['session', 'parent_cancel', 'parent_revision',
    'target_revision', 'target_resolved', 'candidate_unverified', 'decision_unverified',
    'decision_expired', 'changed_request', 'changed_body'])
def test_rejection_authority_or_binding_loss_rolls_back_receipt_and_mutation(db, mutation):
    f = rejection_fixture(db)
    tenant = f['args'][0]
    with db.transaction(force_rollback=True):
        request, body = f['request'], f['body']
        if mutation == 'session':
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (f['args'][3],))
        elif mutation == 'parent_cancel':
            db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, f['parent']))
        elif mutation == 'parent_revision':
            db.execute('UPDATE photo.runs SET revision=revision+1 WHERE tenant_id=%s AND id=%s', (tenant, f['parent']))
        elif mutation == 'target_revision':
            reject_target_with_revision(db, f['selected'][0])
        elif mutation == 'target_resolved':
            reject_target_with_revision(db, f['selected'][0])
        elif mutation == 'candidate_unverified':
            # Candidate document identity belongs to this exact review target.
            aid = json.loads(f['reviews'][0][0][5])['candidate']['artifact_id']
            db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s', (tenant, aid))
        elif mutation == 'decision_unverified':
            db.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s', (tenant, f['artifact']))
        elif mutation == 'decision_expired':
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s", (tenant, f['artifact']))
        elif mutation == 'changed_request': request += ' '
        else: body += ' '
        before = tenant_transaction_state(db, tenant)
        with pytest.raises(psycopg.Error):
            commit_rejection(db, f, request=request, body=body)
        assert tenant_transaction_state(db, tenant) == before
    assert commit_rejection(db, f)['child'] is None


def test_rejection_late_cursor_failure_rolls_back_targets_batch_receipt_events(db):
    f = rejection_fixture(db, selected_count=2)
    tenant = f['args'][0]
    with db.transaction(force_rollback=True):
        db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) '
            'VALUES(%s,9007199254740991) ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor', (tenant,))
        before = tenant_transaction_state(db, tenant)
        with pytest.raises(psycopg.errors.CheckViolation, match='EVENT_CURSOR_EXHAUSTED'):
            commit_rejection(db, f)
        assert tenant_transaction_state(db, tenant) == before
    assert commit_rejection(db, f)['remaining'] == 0


def test_rejection_caller_rollback_restores_complete_state_and_allows_retry(db):
    f = rejection_fixture(db, selected_count=2)
    before = tenant_transaction_state(db, f['args'][0])
    with db.transaction(force_rollback=True):
        assert commit_rejection(db, f)['remaining'] == 0
    assert tenant_transaction_state(db, f['args'][0]) == before
    assert commit_rejection(db, f)['remaining'] == 0


@pytest.mark.parametrize('revision', [0, 9007199254740991, 9007199254740992, 9223372036854775806])
def test_rejection_refuses_nonincrementable_revision_before_any_mutation(db, revision):
    f = rejection_fixture(db)
    changed = dict(f, revision=revision)
    before = tenant_transaction_state(db, f['args'][0])
    with pytest.raises(psycopg.errors.InvalidParameterValue, match='SEMANTIC_REJECTION_INVALID'):
        commit_rejection(db, changed)
    assert tenant_transaction_state(db, f['args'][0]) == before
    assert commit_rejection(db, f)['child'] is None


def test_rejection_last_incrementable_revision_returns_safe_maximum(db):
    f = rejection_fixture(db, parent_revision=9007199254740990)
    result = commit_rejection(db, f)
    assert result['revision'] == '9007199254740991' and result['child'] is None
    assert replay_rejection(db, f) == ('replayed', result)


@pytest.mark.parametrize('mode', ['foreign_actor', 'foreign_session', 'revoked_session'])
def test_rejection_saved_replay_requires_current_actor_and_session(db, mode):
    f = rejection_fixture(db)
    commit_rejection(db, f)
    with db.transaction(force_rollback=True):
        arguments = {}
        if mode == 'foreign_actor': arguments['actor'] = uuid4()
        elif mode == 'foreign_session': arguments['session'] = uuid4()
        else:
            db.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (f['args'][3],))
        before = tenant_transaction_state(db, f['args'][0])
        with pytest.raises(psycopg.errors.InsufficientPrivilege):
            replay_rejection(db, f, **arguments)
        assert tenant_transaction_state(db, f['args'][0]) == before


def test_rejection_commit_keeps_narrow_definer_acl(db):
    signature = 'photo.commit_semantic_rejections(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb)'
    row = db.execute('SELECT pg_get_userbyid(proowner),prosecdef FROM pg_proc '
        'WHERE oid=to_regprocedure(%s)', (signature,)).fetchone()
    assert row == ('photo_fn_owner', True)
    assert db.execute("SELECT has_function_privilege('photo_api',%s,'EXECUTE')", (signature,)).fetchone()[0] is True
    for role in ('photo_worker', 'photo_scheduler', 'photo_gateway', 'photo_portal'):
        assert db.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False


def test_rejection_public_entry_cannot_admit_a_valid_approval_document(db):
    f = rejection_fixture(db, decision='approve')
    before = tenant_transaction_state(db, f['args'][0])
    with pytest.raises(psycopg.errors.SerializationFailure, match='SEMANTIC_REJECTION_TARGET_CONFLICT'):
        commit_rejection(db, f)
    assert tenant_transaction_state(db, f['args'][0]) == before


@pytest.mark.migration_head(2410)
def test_rejection_forward_upgrade_preserves_saved_and_pending_receipts(db):
    from pathlib import Path
    completed = rejection_fixture(db)
    result = commit_rejection(db, completed)
    pending = rejection_fixture(db)
    db.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
        "VALUES(%s,'user',%s,'SemanticIntent.Apply',%s,%s,clock_timestamp()+interval '1 day')",
        (pending['args'][0], pending['args'][2], pending['key'], pending['request_sha']))
    tenants = (completed['args'][0], pending['args'][0])
    before = [tenant_transaction_state(db, tenant) for tenant in tenants]
    signature = 'photo.commit_semantic_rejections(uuid,uuid,uuid,uuid,text,bigint,text,uuid,text,jsonb)'
    authority = db.execute('SELECT oid,proowner,proacl,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',
        (signature,)).fetchone()
    migration = Path(__file__).resolve().parents[2] / 'backend/db/migrations/02411_semantic_rejection_safe_revision.sql'
    # Same complete Up execution as the isolated migration harness; no partial body patch.
    db.execute(migration.read_text().split('-- +goose Down', 1)[0])
    assert db.execute('SELECT oid,proowner,proacl,proconfig FROM pg_proc WHERE oid=to_regprocedure(%s)',
        (signature,)).fetchone() == authority
    assert [tenant_transaction_state(db, tenant) for tenant in tenants] == before
    assert replay_rejection(db, completed) == ('replayed', result)
    assert replay_rejection(db, pending) == ('pending', None)
    adopted = commit_rejection(db, pending, reserve_receipt=False)
    assert adopted['child'] is None
    assert replay_rejection(db, pending) == ('replayed', adopted)

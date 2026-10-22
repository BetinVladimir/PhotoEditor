"""Canonical decision gate over a genuinely published own retouch selection.

Synthetic object version metadata is not physical storage verification. These
scenarios exercise the private transaction component, not public commit replay.
"""
import json
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb
from retouch_copy_apply_fixture import published_decision_fixture
from retouch_copy_apply_fixture import admission_task_fixture
from test_v27_retouch_apply_document_admission import state as base_state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def state(db, tenant):
    # Also detect an accidentally added review batch or altered intent.
    return (base_state(db, tenant), *(db.execute('SELECT to_jsonb(r) FROM photo.' + table
        + ' r WHERE tenant_id=%s ORDER BY to_jsonb(r)::text', (tenant,)).fetchall()
        for table in ('v2_review_batches', 'semantic_intents')))


def gate_inputs(db, *, with_alpha=False):
    data, request, artifact, body = published_decision_fixture(db, with_alpha=with_alpha)
    args = data[0]
    revision = int(json.loads(args[5])['workflow_revision'])
    return data, [*args[:4], revision, request, artifact, body, data[2]['review_documents']]


def lock_decision(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.lock_semantic_shown_retouch_decision_batch('
            + ','.join(['%s']*9) + ')', (*args[:8], Jsonb(args[8]))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('with_alpha', [False, True])
def test_canonical_published_decision_locks_and_replays_without_child(database, with_alpha):
    _, args = gate_inputs(database, with_alpha=with_alpha)
    owner = database.execute('SELECT owner_id FROM photo.backend_documents '
        "WHERE tenant_id=%s AND artifact_id=%s AND purpose='review_selection'",
        (args[0], args[6])).fetchone()[0]
    before = state(database, args[0])
    assert lock_decision(database, args) == owner
    assert lock_decision(database, args) == owner
    assert state(database, args[0]) == before


@pytest.mark.parametrize('mutation,sqlstate', [
    ('request_raw', '23514'), ('body_raw', '23514'), ('artifact', '23514'),
    ('tenant', '42501'), ('intent', '42501'), ('actor', '42501'), ('session', '42501'),
    ('review_document_raw', '23514'), ('invalid_revision', '22023'), ('stale_revision', '40001'),
])
def test_decision_gate_refuses_document_or_scope_substitution(database, mutation, sqlstate):
    _, args = gate_inputs(database, with_alpha=True)
    tenant = args[0]
    assert lock_decision(database, args) is not None
    if mutation == 'request_raw': args[5] += ' '
    elif mutation == 'body_raw': args[7] += ' '
    elif mutation == 'artifact': args[6] = uuid4()
    elif mutation in ('tenant', 'intent', 'actor', 'session'):
        args[('tenant', 'intent', 'actor', 'session').index(mutation)] = uuid4()
    elif mutation == 'invalid_revision': args[4] = 0
    elif mutation == 'stale_revision': args[4] += 1
    else:
        # Same supplied hash and semantically equivalent JSON is still wrong bytes.
        args[8] = [dict(document) for document in args[8]]
        args[8][0]['body'] += ' '
    before = state(database, tenant)
    with pytest.raises(psycopg.Error) as error:
        lock_decision(database, args)
    assert error.value.sqlstate == sqlstate
    assert state(database, tenant) == before


@pytest.mark.parametrize('mutation', ['unverified', 'expired'])
def test_decision_gate_rechecks_published_artifact_liveness(database, mutation):
    _, args = gate_inputs(database, with_alpha=True)
    owner = lock_decision(database, args)
    with database.transaction(force_rollback=True):
        if mutation == 'unverified':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (args[0], args[6]))
        else:
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                'WHERE tenant_id=%s AND id=%s', (args[0], args[6]))
        before = state(database, args[0])
        with pytest.raises(psycopg.Error) as error:
            lock_decision(database, args)
        assert error.value.sqlstate == '23514'
        assert state(database, args[0]) == before
    assert lock_decision(database, args) == owner


def test_decision_lock_gate_remains_private_and_precedes_draft_mutation(database):
    signature = 'photo.lock_semantic_shown_retouch_decision_batch(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb)'
    for role in ('photo_api', 'photo_worker', 'photo_gateway', 'photo_scheduler', 'photo_portal', 'photo_semantic_admission'):
        assert database.execute("SELECT has_function_privilege(%s,%s,'EXECUTE')", (role, signature)).fetchone()[0] is False
    definition = database.execute('SELECT pg_get_functiondef(to_regprocedure(%s))',
        ('photo.create_semantic_shown_retouch_apply_draft_shell(uuid,uuid,uuid,uuid,bigint,text,uuid,text,jsonb,uuid,jsonb)',)).fetchone()[0]
    assert definition.index('photo.lock_semantic_shown_retouch_decision_batch(') < definition.index('INSERT INTO photo.runs(')


@pytest.mark.parametrize('mutation,sqlstate', [
    ('session_revoked', '42501'), ('session_expired', '42501'),
    ('preview_cancelled', '40001'), ('producer_cancelled', '40001'),
    ('working_image_unverified', '40001'), ('presentation_expired', '40001'),
])
def test_decision_gate_refuses_late_selection_authority_loss(database, mutation, sqlstate):
    data, args = gate_inputs(database, with_alpha=True)
    owner = lock_decision(database, args)
    selected = json.loads(args[7])['targets'][0]['preview']
    with database.transaction(force_rollback=True):
        if mutation == 'session_revoked':
            database.execute('UPDATE photo.sessions SET revoked_at=clock_timestamp() WHERE id=%s', (args[3],))
        elif mutation == 'session_expired':
            # authorize_tenant uses now(), frozen at this transaction's start.
            database.execute("UPDATE photo.sessions SET expires_at=transaction_timestamp()-interval '1 second' WHERE id=%s", (args[3],))
        elif mutation == 'preview_cancelled':
            database.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (args[0], data[1]))
        elif mutation == 'producer_cancelled':
            database.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (args[0], data[7]))
        elif mutation == 'working_image_unverified':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (args[0], selected['render']['artifact_id']))
        else:
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                'WHERE tenant_id=%s AND id=%s', (args[0], selected['evidence']['artifact_id']))
        before = state(database, args[0])
        with pytest.raises(psycopg.Error) as error:
            lock_decision(database, args)
        assert error.value.sqlstate == sqlstate
        assert state(database, args[0]) == before
    assert lock_decision(database, args) == owner


@pytest.mark.parametrize('with_alpha', [False, True])
def test_decision_gate_refuses_target_advanced_by_actual_draft_transition(database, with_alpha):
    # Do not bypass the immutable target binding or state/revision triggers.
    values, task = admission_task_fixture(database, with_alpha=with_alpha)
    data, request, artifact, body, child = values[:5]
    original = data[0]
    revision = int(json.loads(original[5])['workflow_revision'])
    args = [*original[:4], revision, request, artifact, body, data[2]['review_documents']]
    owner = lock_decision(database, args)
    with database.transaction(force_rollback=True):
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            draft = database.execute('SELECT photo.create_semantic_shown_retouch_apply_draft_shell('
                + ','.join(['%s']*11) + ')',
                (*args[:8], Jsonb(args[8]), child, Jsonb([task]))).fetchone()[0]
            database.execute('RESET ROLE')
        assert draft is not None
        target = database.execute('SELECT state,revision,child_run_id FROM photo.semantic_targets '
            'WHERE tenant_id=%s AND id=%s', (args[0], original[4])).fetchone()
        assert target == ('queued', 2, child)
        before = state(database, args[0])
        with pytest.raises(psycopg.Error) as error:
            lock_decision(database, args)
        assert error.value.sqlstate == '40001'
        assert state(database, args[0]) == before
    assert lock_decision(database, args) == owner

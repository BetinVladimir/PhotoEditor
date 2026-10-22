"""Exact own Apply input registration/sealing before activation."""
from copy import deepcopy
import json
from uuid import uuid4

import psycopg
import pytest
from psycopg.types.json import Jsonb
from test_v27_retouch_apply_reserved_draft import prepared, reserve
from test_v27_retouch_apply_decision_gate import state as base_state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def snapshot(db, tenant):
    return (base_state(db, tenant), db.execute('SELECT to_jsonb(i) FROM photo.v2_task_inputs i '
        'WHERE tenant_id=%s ORDER BY to_jsonb(i)::text', (tenant,)).fetchall())


def call(db, function, args):
    assert function in ('register_semantic_shown_retouch_apply_inputs',
        'seal_semantic_shown_retouch_apply_inputs', 'semantic_shown_retouch_apply_sealed_inputs_ready')
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.' + function + '(' + ','.join(['%s']*5) + ')', args).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def draft(db, *, with_alpha=False):
    values, task, args = prepared(db, with_alpha=with_alpha)
    assert reserve(db, args) is not None
    fixed = (args[0], values[0][2]['project'], args[2], values[4])
    return task, (*fixed, Jsonb([task]))


@pytest.mark.parametrize('with_alpha', [False, True])
def test_exact_registration_replay_and_sealing_leave_child_inactive(database, with_alpha):
    task, args = draft(database, with_alpha=with_alpha)
    count = len(task['inventory']['entries']) + 2
    assert call(database, 'register_semantic_shown_retouch_apply_inputs', args) == count
    before = snapshot(database, args[0])
    assert call(database, 'register_semantic_shown_retouch_apply_inputs', args) == count
    assert snapshot(database, args[0]) == before
    assert call(database, 'seal_semantic_shown_retouch_apply_inputs', args) == 1
    assert call(database, 'semantic_shown_retouch_apply_sealed_inputs_ready', args) is True
    rows = database.execute('SELECT artifact_id,sha256,contract,ordinal FROM photo.v2_task_inputs '
        'WHERE tenant_id=%s AND task_id=%s ORDER BY ordinal', (args[0], task['node']['id'])).fetchall()
    expected = [(entry['ref']['artifact_id'], entry['ref']['sha256'], entry['contract'], ordinal)
        for ordinal, entry in enumerate(task['inventory']['entries'])]
    expected += [(task['transport']['chunks'][0]['artifact_id'], task['transport']['chunks'][0]['sha256'], 'InputClosureChunk', 1000000),
        (task['transport']['manifest']['artifact_id'], task['transport']['manifest']['sha256'], 'InputClosureManifest', 1000001)]
    assert [(str(aid), sha, contract, ordinal) for aid, sha, contract, ordinal in rows] == expected
    assert database.execute('SELECT status,graph_sealed FROM photo.runs WHERE tenant_id=%s AND id=%s',
        args[:1] + args[3:4]).fetchone() == ('draft', False)
    assert database.execute('SELECT state,attempt FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',
        args[:1] + args[3:4]).fetchall() == [('blocked', 0)]


@pytest.mark.parametrize('mutation', ['missing_input', 'duplicate_input', 'input_order', 'manifest', 'params', 'node_digest'])
def test_changed_input_projection_refuses_without_partial_registration(database, mutation):
    task, args = draft(database, with_alpha=True)
    changed = deepcopy(task)
    if mutation == 'missing_input': changed['inventory']['entries'].pop()
    elif mutation == 'duplicate_input': changed['inventory']['entries'].append(changed['inventory']['entries'][0])
    elif mutation == 'input_order': changed['inventory']['entries'].reverse()
    elif mutation == 'manifest': changed['transport']['manifest']['sha256'] = 'e'*64
    elif mutation == 'params':
        params = json.loads(changed['params_body'])
        params['child_run_id'] = str(uuid4())
        changed['params_body'] = json.dumps(params, sort_keys=True, separators=(',', ':'))
    else: changed['node']['input_digest'] = 'e'*64
    before = snapshot(database, args[0])
    with pytest.raises(psycopg.Error) as error:
        call(database, 'seal_semantic_shown_retouch_apply_inputs', (*args[:4], Jsonb([changed])))
    assert error.value.sqlstate in ('23514', '40001')
    assert snapshot(database, args[0]) == before
    assert call(database, 'seal_semantic_shown_retouch_apply_inputs', args) == 1


@pytest.mark.parametrize('mutation', ['params_unverified', 'chunk_expired', 'missing_registered_input'])
def test_sealing_rechecks_live_and_registered_inputs(database, mutation):
    task, args = draft(database, with_alpha=True)
    assert call(database, 'register_semantic_shown_retouch_apply_inputs', args) == len(task['inventory']['entries']) + 2
    with database.transaction(force_rollback=True):
        if mutation == 'params_unverified':
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',
                (args[0], task['params_artifact_id']))
        elif mutation == 'chunk_expired':
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' "
                'WHERE tenant_id=%s AND id=%s', (args[0], task['transport']['chunks'][0]['artifact_id']))
        else:
            # DELETE is permitted here; UPDATE is deliberately immutable. Never
            # disable the real input writer/consumer guards for this scenario.
            database.execute('DELETE FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND ordinal=0',
                (args[0], task['node']['id']))
        before = snapshot(database, args[0])
        with pytest.raises(psycopg.Error) as error:
            call(database, 'seal_semantic_shown_retouch_apply_inputs', args)
        assert error.value.sqlstate in ('23514', '40001')
        assert snapshot(database, args[0]) == before
    assert call(database, 'seal_semantic_shown_retouch_apply_inputs', args) == 1


def activate(db, args):
    parent = db.execute('SELECT w.run_id FROM photo.v2_review_batches b JOIN photo.v2_workflows w '
        'ON w.tenant_id=b.tenant_id AND w.id=b.workflow_id WHERE b.tenant_id=%s AND b.child_run_id=%s',
        (args[0], args[3])).fetchone()[0]
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.activate_semantic_shown_retouch_apply_child(%s,%s,%s,%s,%s,%s)',
            (args[0], args[1], parent, args[3], args[2], args[4])).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('mutation', ['unsealed', 'input_unverified', 'missing_registered_input'])
def test_activation_requires_complete_live_sealed_closure(database, mutation):
    task, args = draft(database, with_alpha=True)
    if mutation != 'unsealed':
        assert call(database, 'seal_semantic_shown_retouch_apply_inputs', args) == 1
        assert call(database, 'semantic_shown_retouch_apply_sealed_inputs_ready', args) is True
    with database.transaction(force_rollback=True):
        if mutation == 'input_unverified':
            pixels = next(entry['ref']['artifact_id'] for entry in task['inventory']['entries'] if entry['contract'] == 'NpyArray')
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s', (args[0], pixels))
        elif mutation == 'missing_registered_input':
            database.execute('DELETE FROM photo.v2_task_inputs WHERE tenant_id=%s AND task_id=%s AND ordinal=0',
                (args[0], task['node']['id']))
        before = snapshot(database, args[0])
        assert call(database, 'semantic_shown_retouch_apply_sealed_inputs_ready', args) is False
        assert activate(database, args) is False
        assert snapshot(database, args[0]) == before
    if mutation != 'unsealed':
        assert call(database, 'semantic_shown_retouch_apply_sealed_inputs_ready', args) is True


@pytest.mark.parametrize('schema,expected', [
    ('886cb3e1b7316c40d6696ab57e59bcc2a8ec714b50ffd8d51c0a1cb9cf799f90', True),
    ('7593be39f45ce3bf1aa8c5b3c42bc9defc82f2416def579fd2d3e6d3ea165ab0', True),
    ('0'*64, False), ('', False), (None, False),
])
def test_sealed_retouch_copy_retains_safe_historical_pin_without_rewriting_documents(database,schema,expected):
    task,args=draft(database)
    assert call(database,'seal_semantic_shown_retouch_apply_inputs',args)==1
    task_id=task['node']['id']
    current=database.execute("SELECT execution_profile->>'contracts_sha' FROM photo.tasks WHERE id=%s",(task_id,)).fetchone()[0]
    assert current=='75e3bcf5679402efe777b409f919bdcd594f92be5a163975ccf66e27dc3b6ee8'
    assert call(database,'semantic_shown_retouch_apply_sealed_inputs_ready',args) is True
    # Isolate persisted compatibility on an actual sealed safe-numeric task.
    # Published documents, hashes and inputs remain unchanged.
    if schema is None:
        database.execute("UPDATE photo.tasks SET execution_profile=execution_profile-'contracts_sha' WHERE id=%s",(task_id,))
    else:
        database.execute("UPDATE photo.tasks SET execution_profile=jsonb_set(execution_profile,'{contracts_sha}',%s::jsonb) WHERE id=%s",(Jsonb(schema),task_id))
    before=snapshot(database,args[0])
    assert call(database,'semantic_shown_retouch_apply_sealed_inputs_ready',args) is expected
    assert snapshot(database,args[0])==before

"""Exact pure decision/task membership, separate from live Apply authority.

The first task comes from actual own retouch publication. Additional tasks are
synthetic projections for the immutable membership predicate only: they are not
published, admitted, qualified or executed and prove no multi-target authority.
"""
from copy import deepcopy
from hashlib import sha256
import json
from uuid import UUID, uuid4, uuid5

import pytest
from psycopg.types.json import Jsonb
from retouch_copy_apply_fixture import admission_task_fixture
from test_v27_retouch_apply_document_admission import state
from test_v27_retouch_preview_sql_roles import database, isolated_database_config


def encode(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'))


def digest(value):
    return sha256(encode(value).encode()).hexdigest()


def projected_task(original, child, target):
    """Rebuild every pure transport identity/hash for a second target.

    Shared source references are deliberate: this predicate checks membership,
    while the subsequent live document gate checks approved-edit target scope.
    """
    row = deepcopy(original)
    params = json.loads(row['params_body'])
    params['target_id'] = str(target)
    task = uuid5(child, 'semantic.adopt_shown_retouch_region:' + str(target))
    params_id = str(uuid5(task, 'Params'))
    row.update(target_id=str(target), params_body=encode(params),
        params_sha=digest(params), params_artifact_id=params_id)
    entries = row['inventory']['entries']
    for entry in entries:
        if entry['contract'] == 'Params':
            entry['ref'] = dict(artifact_id=params_id, sha256=row['params_sha'])
    entries.sort(key=lambda entry: UUID(entry['ref']['artifact_id']).int)
    entries_sha = digest(entries)
    chunk = dict(artifact_id=str(uuid5(task, 'v2_input_chunk_0')),
        sha256=digest(dict(entries=entries, schema_version=1)))
    manifest = dict(chunks=[chunk], entry_count=len(entries), entries_sha=entries_sha,
        schema_version=1, task_id=str(task))
    row['inventory'] = dict(task_id=str(task), entries=entries)
    row['transport'] = dict(task_id=str(task), entry_count=len(entries),
        entries_sha=entries_sha, chunks=[chunk],
        manifest=dict(artifact_id=str(uuid5(task, 'v2_input_manifest')), sha256=digest(manifest)),
        direct_roots=[entry['ref'] for entry in entries])
    row['node'].update(id=str(task),
        node_key='semantic.adopt_shown_retouch_region:' + str(target),
        params_artifact_id=params_id, params_sha=row['params_sha'],
        input_digest=digest(dict(entries=entries, operation='semantic.adopt_shown_retouch_region',
            task=str(task), version='1')))
    return row


def bound(db, child, decisions, tasks):
    # Private helper; invoke through its owner, never grant a runtime role.
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.semantic_shown_retouch_batch_nodes_bound(%s,%s,%s)',
            (child, Jsonb(decisions), Jsonb(tasks))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def batch(db, *, with_alpha=False):
    values, original = admission_task_fixture(db, with_alpha=with_alpha)
    child = values[4]
    first = json.loads(values[3])['targets'][0]
    second_task = projected_task(original, child, uuid4())
    second = deepcopy(first)
    second['target_id'] = second_task['target_id']
    rejected = dict(target_id=str(uuid4()), decision='reject')
    return values, [first, second, rejected], [original, second_task]


@pytest.mark.parametrize('with_alpha', [False, True])
def test_membership_accepts_published_single_and_projected_multiple_targets(database, with_alpha):
    values, decisions, tasks = batch(database, with_alpha=with_alpha)
    before = state(database, values[0][0][0])
    assert bound(database, values[4], decisions[:1], tasks[:1]) is True
    assert bound(database, values[4], decisions, tasks) is True
    # Correspondence is by target identity, not the client's array order.
    assert bound(database, values[4], list(reversed(decisions)), list(reversed(tasks))) is True
    assert bound(database, values[4], decisions, tasks) is True
    assert state(database, values[0][0][0]) == before
    assert database.execute('SELECT count(*) FROM photo.runs WHERE id=%s', (values[4],)).fetchone() == (0,)


@pytest.mark.parametrize('mutation', [
    'missing_task', 'extra_task', 'duplicate_task', 'duplicate_decision', 'unknown_target',
    'reject_has_task', 'missing_decision', 'foreign_child', 'params_child', 'params_target',
    'params_raw', 'params_sha', 'params_id', 'node_id', 'node_params', 'node_sha',
    'node_operation', 'manifest', 'inventory_task', 'missing_input', 'render', 'evidence',
    'missing_preview', 'extra_task_field',
])
def test_membership_refuses_incomplete_or_changed_batch_without_mutation(database, mutation):
    values, decisions, tasks = batch(database, with_alpha=True)
    child = values[4]
    assert bound(database, child, decisions, tasks) is True
    if mutation == 'missing_task': tasks.pop()
    elif mutation == 'extra_task': tasks.append(projected_task(tasks[0], child, uuid4()))
    elif mutation == 'duplicate_task': tasks[1] = deepcopy(tasks[0])
    elif mutation == 'duplicate_decision': decisions[1] = deepcopy(decisions[0])
    elif mutation == 'unknown_target': tasks[1]['target_id'] = str(uuid4())
    elif mutation == 'reject_has_task': decisions[1]['decision'] = 'reject'
    elif mutation == 'missing_decision': decisions.pop(1)
    elif mutation == 'foreign_child': child = uuid4()
    elif mutation in ('params_child', 'params_target'):
        params = json.loads(tasks[1]['params_body'])
        params['child_run_id' if mutation == 'params_child' else 'target_id'] = str(uuid4())
        tasks[1]['params_body'] = encode(params)
        tasks[1]['params_sha'] = digest(params)
    elif mutation == 'params_raw': tasks[1]['params_body'] += ' '
    elif mutation == 'params_sha': tasks[1]['params_sha'] = 'e'*64
    elif mutation == 'params_id': tasks[1]['params_artifact_id'] = str(uuid4())
    elif mutation == 'node_id': tasks[1]['node']['id'] = str(uuid4())
    elif mutation == 'node_params': tasks[1]['node']['params_artifact_id'] = str(uuid4())
    elif mutation == 'node_sha': tasks[1]['node']['params_sha'] = 'e'*64
    elif mutation == 'node_operation': tasks[1]['node']['operation'] = 'semantic.adopt_shown_remove_object'
    elif mutation == 'manifest': tasks[1]['transport']['manifest']['sha256'] = 'e'*64
    elif mutation == 'inventory_task': tasks[1]['inventory']['task_id'] = str(uuid4())
    elif mutation == 'missing_input': tasks[1]['inventory']['entries'].pop()
    elif mutation in ('render', 'evidence'): decisions[1]['preview'][mutation]['sha256'] = 'e'*64
    elif mutation == 'missing_preview': del decisions[1]['preview']
    else: tasks[1]['extra'] = True
    before = state(database, values[0][0][0])
    assert bound(database, child, decisions, tasks) is False
    assert state(database, values[0][0][0]) == before

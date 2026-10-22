"""Own same-review multi-target SQL gate; metadata only, never physical QA."""
import psycopg
import pytest
from psycopg.types.json import Jsonb
from removal_multi_selection_fixture import multi_selection_fixture, publish_removal_decision_bundles
from test_v27_removal_preview_sql_roles import database, isolated_database_config


def lock_decision(db, args):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_fn_owner')
        result = db.execute('SELECT photo.lock_semantic_shown_removal_decision_batch('
            + ','.join(['%s'] * 9) + ')', (*args[:8], Jsonb(args[8]))).fetchone()[0]
        db.execute('RESET ROLE')
        return result


def state(db, tenant):
    tables = ('runs', 'tasks', 'task_results', 'artifacts', 'semantic_targets',
              'semantic_intents', 'v2_workflows', 'v2_review_batches', 'backend_documents')
    return tuple(db.execute('SELECT to_jsonb(r) FROM photo.' + table
                 + ' r WHERE tenant_id=%s ORDER BY to_jsonb(r)::text', (tenant,)).fetchall()
                 for table in tables)


def test_multi_removal_gate_preserves_all_targets_and_explicit_order(database):
    bundles, args = multi_selection_fixture(database)
    before = state(database, args[0])
    owner = lock_decision(database, args)
    assert owner is not None and state(database, args[0]) == before
    assert lock_decision(database, args) == owner
    request, artifact, body = publish_removal_decision_bundles(database, bundles, reverse_targets=True)
    reverse = [*args[:5], request, artifact, body, args[8]]
    before = state(database, args[0])
    assert lock_decision(database, reverse) is not None
    assert state(database, args[0]) == before


@pytest.mark.parametrize('index', [0, 1])
@pytest.mark.parametrize('loss', ['canonical_result', 'working_image'])
def test_multi_removal_gate_refuses_whole_selection_when_one_target_is_lost(database, index, loss):
    bundles, args = multi_selection_fixture(database)
    assert lock_decision(database, args) is not None
    bundle = sorted(bundles, key=lambda value: str(value['target']))[index]
    data = bundle['data']
    if loss == 'canonical_result':
        database.execute('UPDATE photo.tasks SET canonical_result_id=NULL WHERE id=%s', (data[7],))
    else:
        receipt = database.execute('SELECT v27_removal_preview_receipt FROM photo.runs WHERE id=%s', (data[1],)).fetchone()[0]
        database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE id=%s', (receipt['working_image']['artifact_id'],))
    before = state(database, args[0])
    with pytest.raises((psycopg.errors.SerializationFailure, psycopg.errors.InsufficientPrivilege)):
        lock_decision(database, args)
    assert state(database, args[0]) == before

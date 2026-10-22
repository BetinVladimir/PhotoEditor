"""Shown Apply permanent rejection: real SQL, disposable metadata fixture.

Synthetic canonical output metadata; no physical copy/storage claim. Authored only.
"""
from uuid import uuid4
import pytest
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture, adopt
from test_v27_semantic_child_canonical_completion import finalize
from removal_transaction_state import tenant_transaction_state


def reject(db, tenant, task, result):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        accepted = db.execute("SELECT photo.reject_v2_adoption(%s,%s,%s,'SEMANTIC_SHOWN_BYTES_INVALID')",
                              (tenant, task, result)).fetchone()[0]
        db.execute('RESET ROLE')
        return accepted


def test_permanent_shown_rejection_reaches_failed_child_and_replays_without_effects(db):
    values, task, commit = successful_copy_metadata_fixture(db)
    tenant, task_id, _, _, result_id = commit[:5]
    child = values[4]
    assert finalize(db, tenant, child) == 'pending'
    assert reject(db, tenant, task_id, result_id) is True
    assert db.execute('SELECT status,cancel_requested FROM photo.runs WHERE tenant_id=%s AND id=%s',
                      (tenant, child)).fetchone() == ('cancelling', True)
    before = tenant_transaction_state(db, tenant)
    assert reject(db, tenant, task_id, result_id) is False
    assert tenant_transaction_state(db, tenant) == before
    # The recorded adoption error takes precedence over cancellation without
    # requiring an acceptance marker for this rejected succeeded result.
    assert finalize(db, tenant, child) == 'failed'
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                      (tenant, child)).fetchall() == [('failed',)]
    for event in ('v2.adoption.failed', 'semantic.child.failed'):
        assert db.execute('SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type=%s',
                          (tenant, child, event)).fetchone() == (1,)
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == 'failed'
    assert reject(db, tenant, task_id, result_id) is False
    assert tenant_transaction_state(db, tenant) == after


@pytest.mark.parametrize('mode', ['wrong_result', 'foreign_tenant', 'unsealed',
                                  'missing_nodes', 'wrong_node', 'already_adopted', 'cancelled'])
def test_shown_rejection_requires_bound_current_unadopted_child(db, mode):
    values, task, commit = successful_copy_metadata_fixture(db)
    tenant, task_id, _, _, result_id = commit[:5]
    child = values[4]
    supplied_tenant, supplied_result = tenant, result_id
    if mode == 'wrong_result': supplied_result = uuid4()
    elif mode == 'foreign_tenant': supplied_tenant = uuid4()
    elif mode == 'unsealed':
        db.execute('UPDATE photo.runs SET graph_sealed=false WHERE tenant_id=%s AND id=%s', (tenant, child))
    elif mode in ('missing_nodes', 'wrong_node'):
        expression = "'[]'::jsonb" if mode == 'missing_nodes' else "'[{}]'::jsonb"
        db.execute("UPDATE photo.runs SET config_snapshot=jsonb_set(config_snapshot,'{shown_semantic,nodes}',"
                   + expression + ') WHERE tenant_id=%s AND id=%s', (tenant, child))
    elif mode == 'already_adopted': assert adopt(db, commit) is True
    else: db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, child))
    before = tenant_transaction_state(db, tenant)
    assert reject(db, supplied_tenant, task_id, supplied_result) is False
    assert tenant_transaction_state(db, tenant) == before


def test_user_cancel_without_adoption_failure_remains_cancelled(db):
    values, task, commit = successful_copy_metadata_fixture(db)
    tenant, child = commit[0], values[4]
    db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, child))
    assert finalize(db, tenant, child) == 'cancelled'
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                      (tenant, child)).fetchall() == [('cancelled',)]
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == 'cancelled'
    assert tenant_transaction_state(db, tenant) == after


@pytest.mark.migration_head(2415)
def test_shown_rejection_upgrade_preserves_identity_acl_and_tenant_rows(db):
    from pathlib import Path
    values, task, commit = successful_copy_metadata_fixture(db,historical_writer_head=2415)
    tenant, task_id, _, _, result_id = commit[:5]
    assert reject(db, tenant, task_id, result_id) is False
    signature = 'photo.reject_v2_adoption(uuid,uuid,uuid,text)'
    query = 'SELECT oid,proowner,proacl,proconfig,prosecdef FROM pg_proc WHERE oid=to_regprocedure(%s)'
    identity = db.execute(query, (signature,)).fetchone()
    before = tenant_transaction_state(db, tenant)
    migration = Path(__file__).resolve().parents[2] / 'backend/db/migrations/02416_shown_child_adoption_rejection.sql'
    db.execute(migration.read_text().split('-- +goose Down', 1)[0])
    assert db.execute(query, (signature,)).fetchone() == identity
    assert tenant_transaction_state(db, tenant) == before
    assert reject(db, tenant, task_id, result_id) is True
    assert finalize(db, tenant, values[4]) == 'failed'


def test_active_worker_cancellation_waits_then_settles_once(db):
    # Helper performs real claim, observes pending while lease is active, then
    # commits task failure through the worker role; uses this disposable db.
    from test_v27_retouch_child_finalization import cancelled_worker_fixture
    tenant, task_id, child = cancelled_worker_fixture(db)
    assert finalize(db, tenant, child) == 'cancelled'
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == 'cancelled'
    assert tenant_transaction_state(db, tenant) == after

"""Target transition guards isolated from child processing, disposable SQL only.

Real retouch Apply admission; explicit child terminal metadata below isolates
settlement guards and does not claim canonical worker/adoption completion.
"""
from uuid import uuid4
import psycopg
import pytest
from retouch_copy_apply_fixture import committed_copy_fixture
from removal_transaction_state import tenant_transaction_state


def fixture(db):
    values, task, response = committed_copy_fixture(db)
    return values[0][0][0], values[4], task['target_id']


def settle(db, tenant, child):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        count = db.execute('SELECT photo.settle_semantic_child(%s,%s)', (tenant, child)).fetchone()[0]
        db.execute('RESET ROLE')
        return count


@pytest.mark.parametrize('child_status,target_state', [('completed','applied'), ('failed','failed'), ('cancelled','cancelled')])
def test_settlement_increments_target_once_and_preserves_bindings(db, child_status, target_state):
    tenant, child, target = fixture(db)
    before = db.execute('SELECT revision,child_run_id,batch_id,review_id,override_id FROM photo.semantic_targets WHERE id=%s', (target,)).fetchone()
    db.execute('UPDATE photo.runs SET status=%s WHERE tenant_id=%s AND id=%s', (child_status, tenant, child))
    assert settle(db, tenant, child) == 1
    assert db.execute('SELECT state,revision,child_run_id,batch_id,review_id,override_id FROM photo.semantic_targets WHERE id=%s', (target,)).fetchone() == (target_state, before[0]+1, *before[1:])
    after = tenant_transaction_state(db, tenant)
    assert settle(db, tenant, child) == 0
    assert tenant_transaction_state(db, tenant) == after


@pytest.mark.parametrize('target_state', ['applied','failed','cancelled'])
def test_target_cannot_finish_before_its_child(db, target_state):
    tenant, child, target = fixture(db)
    before = tenant_transaction_state(db, tenant)
    with pytest.raises(psycopg.errors.CheckViolation, match='SEMANTIC_CHILD_NOT_TERMINAL'):
        with db.transaction():
            db.execute('UPDATE photo.semantic_targets SET state=%s WHERE id=%s', (target_state, target))
    assert tenant_transaction_state(db, tenant) == before


@pytest.mark.parametrize('column', ['child_run_id','batch_id','review_id','override_id'])
def test_terminal_transition_cannot_replace_admitted_binding(db, column):
    tenant, child, target = fixture(db)
    db.execute("UPDATE photo.runs SET status='completed' WHERE tenant_id=%s AND id=%s", (tenant, child))
    before = tenant_transaction_state(db, tenant)
    with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState, match='SEMANTIC_TARGET_CHILD_BINDING_IMMUTABLE'):
        with db.transaction():
            db.execute(f"UPDATE photo.semantic_targets SET state='applied',{column}=%s WHERE id=%s", (uuid4(), target))
    assert tenant_transaction_state(db, tenant) == before


def test_terminal_target_cannot_be_reopened(db):
    tenant, child, target = fixture(db)
    db.execute("UPDATE photo.runs SET status='completed' WHERE tenant_id=%s AND id=%s", (tenant, child))
    assert settle(db, tenant, child) == 1
    before = tenant_transaction_state(db, tenant)
    with pytest.raises(psycopg.errors.ObjectNotInPrerequisiteState, match='SEMANTIC_TARGET_STATE_CONFLICT'):
        with db.transaction():
            db.execute("UPDATE photo.semantic_targets SET state='queued' WHERE id=%s", (target,))
    assert tenant_transaction_state(db, tenant) == before

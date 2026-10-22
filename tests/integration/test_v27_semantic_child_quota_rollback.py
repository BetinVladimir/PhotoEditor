"""Own child quota/atomic rollback matrix; authored and unexecuted.

Real retouch Apply/claim/adoption SQL; synthetic output metadata, disposable db.
"""
import psycopg
import pytest
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture, adopt
from test_v27_semantic_child_adoption_rejection import reject
from test_v27_semantic_child_canonical_completion import finalize
from removal_transaction_state import tenant_transaction_state


def ready(db, outcome):
    values, task, commit = successful_copy_metadata_fixture(db)
    tenant, task_id, _, _, result = commit[:5]
    child = values[4]
    if outcome == 'completed': assert adopt(db, commit) is True
    elif outcome == 'failed': assert reject(db, tenant, task_id, result) is True
    else: db.execute('UPDATE photo.runs SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (tenant, child))
    return tenant, child


def quota(db, tenant, child):
    return db.execute("SELECT state,bytes FROM photo.quota_reservations WHERE tenant_id=%s "
                      "AND holder_type='run' AND holder_id=%s", (tenant, child)).fetchone()


@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
def test_child_settlement_accounts_remaining_reservation_once(db, outcome):
    tenant, child = ready(db, outcome)
    state, remaining = quota(db, tenant, child)
    assert state == 'reserved'
    reserved = db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone()[0]
    assert finalize(db, tenant, child) == outcome
    assert quota(db, tenant, child) == ('consumed' if outcome == 'completed' else 'released', 0)
    assert db.execute('SELECT reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s', (tenant,)).fetchone() == (reserved - remaining,)
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == outcome
    assert tenant_transaction_state(db, tenant) == after


@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
@pytest.mark.parametrize('fault,code', [('cursor', 'EVENT_CURSOR_EXHAUSTED'),
                                       ('revision', 'REVISION_EXHAUSTED'),
                                       ('usage', 'SEMANTIC_RESERVATION_INCONSISTENT')])
def test_child_late_failure_rolls_back_all_tenant_rows_then_retries(db, outcome, fault, code):
    tenant, child = ready(db, outcome)
    with db.transaction(force_rollback=True):
        if fault == 'cursor':
            db.execute('INSERT INTO photo.tenant_event_cursors(tenant_id,last_cursor) VALUES(%s,9007199254740991) '
                       'ON CONFLICT(tenant_id) DO UPDATE SET last_cursor=excluded.last_cursor', (tenant,))
        elif fault == 'revision':
            db.execute('UPDATE photo.runs SET revision=9007199254740991 WHERE tenant_id=%s AND id=%s', (tenant, child))
        else:
            # Guarantee an underflow even if the adopted output exhausted its reserve.
            db.execute("UPDATE photo.quota_reservations SET bytes=greatest(bytes,1) WHERE tenant_id=%s "
                       "AND holder_type='run' AND holder_id=%s", (tenant, child))
            db.execute('UPDATE photo.tenant_usage SET reserved_bytes=0 WHERE tenant_id=%s', (tenant,))
        before = tenant_transaction_state(db, tenant)
        with pytest.raises(psycopg.errors.CheckViolation, match=code):
            finalize(db, tenant, child)
        assert tenant_transaction_state(db, tenant) == before
    assert finalize(db, tenant, child) == outcome
    assert quota(db, tenant, child) == ('consumed' if outcome == 'completed' else 'released', 0)


@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
def test_caller_rollback_restores_entire_child_settlement(db, outcome):
    tenant, child = ready(db, outcome)
    before = tenant_transaction_state(db, tenant)
    with db.transaction(force_rollback=True):
        assert finalize(db, tenant, child) == outcome
        assert tenant_transaction_state(db, tenant) != before
    assert tenant_transaction_state(db, tenant) == before
    assert finalize(db, tenant, child) == outcome
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == outcome
    assert tenant_transaction_state(db, tenant) == after

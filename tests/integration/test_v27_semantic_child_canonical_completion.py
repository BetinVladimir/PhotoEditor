"""Own disposable child finalization scenarios; authored, not executed.

Real retouch Apply/dispatch/claim and SQL adoption. Output metadata is synthetic:
this suite does not assert physical image execution or object-store validation.
Uses conftest db; never imports a legacy external database fixture.
"""
import pytest
import psycopg
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture, adopt
from removal_transaction_state import tenant_transaction_state


def finalize(db, tenant, child):
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        result = db.execute('SELECT photo.finalize_shown_semantic_apply_child(%s,%s)',
                            (tenant, child)).fetchone()[0]
        db.execute('RESET ROLE')
        return result


@pytest.mark.parametrize('mode', ['not_adopted', 'missing_marker', 'missing_result',
                                  'wrong_attempt', 'wrong_fence'])
def test_child_waits_for_current_canonical_adopted_result_without_mutation(db, mode):
    values, task, commit = successful_copy_metadata_fixture(db)
    tenant, task_id, _, _, result_id = commit[:5]
    child = values[4]
    if mode != 'not_adopted':
        assert adopt(db, commit) is True
    # Deliberate privileged metadata corruption isolates each finalizer predicate.
    # Roll it back before normal finalization so the retry uses the admitted result.
    with db.transaction(force_rollback=True):
        if mode == 'missing_marker':
            db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt' "
                       'AND event_id=photo.v2_adoption_event_id(%s,%s)',
                       (tenant, task_id, result_id))
        elif mode == 'missing_result':
            before=tenant_transaction_state(db,tenant)
            with pytest.raises(psycopg.errors.CheckViolation) as refused:
                with db.transaction():
                    db.execute('UPDATE photo.tasks SET canonical_result_id=NULL WHERE tenant_id=%s AND id=%s',
                               (tenant,task_id))
            assert refused.value.sqlstate=='23514'
            assert refused.value.diag.constraint_name=='tasks_domain_4'
            assert tenant_transaction_state(db,tenant)==before
        elif mode in ('wrong_attempt', 'wrong_fence'):
            column = 'attempt' if mode == 'wrong_attempt' else 'fencing_token'
            db.execute(f'UPDATE photo.tasks SET {column}={column}+1 WHERE tenant_id=%s AND id=%s',
                       (tenant, task_id))
        before = tenant_transaction_state(db, tenant)
        if mode!='missing_result':
            assert finalize(db, tenant, child) == 'pending'
        assert tenant_transaction_state(db, tenant) == before
    if mode == 'not_adopted':
        assert adopt(db, commit) is True
    revision = db.execute('SELECT revision FROM photo.runs WHERE tenant_id=%s AND id=%s',
                          (tenant, child)).fetchone()[0]
    assert finalize(db, tenant, child) == 'completed'
    assert db.execute('SELECT status,revision FROM photo.runs WHERE tenant_id=%s AND id=%s',
                      (tenant, child)).fetchone() == ('completed', revision + 1)
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                      (tenant, child)).fetchall() == [('applied',)]
    after = tenant_transaction_state(db, tenant)
    assert finalize(db, tenant, child) == 'completed'
    assert tenant_transaction_state(db, tenant) == after


def test_installed_scheduler_reaches_adopted_retouch_child(db):
    values, task, commit = successful_copy_metadata_fixture(db)
    tenant, child = commit[0], values[4]
    assert adopt(db, commit) is True
    with db.transaction():
        db.execute('SET LOCAL ROLE photo_scheduler')
        assert db.execute('SELECT photo.finalize_next_semantic_apply_child()').fetchone() == (1,)
        assert db.execute('SELECT photo.finalize_next_semantic_apply_child()').fetchone() == (0,)
        db.execute('RESET ROLE')
    assert db.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s',
                      (tenant, child)).fetchone() == ('completed',)
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s "
                      "AND type='semantic.child.completed'", (tenant, child)).fetchone() == (1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s "
                      "AND routing_key='semantic.child.completed' AND payload->>'run_id'=%s",
                      (tenant, str(child))).fetchone() == (1,)


def test_child_without_result_waits_in_legal_queued_state(db):
    from retouch_copy_apply_fixture import committed_copy_fixture
    values,task,_=committed_copy_fixture(db)
    tenant=values[0][0][0];child=values[4]
    rows=db.execute('SELECT state,canonical_result_id FROM photo.tasks WHERE tenant_id=%s AND run_id=%s',(tenant,child)).fetchall()
    assert rows and all(state=='queued' and result is None for state,result in rows)
    before=tenant_transaction_state(db,tenant)
    assert finalize(db,tenant,child)=='pending'
    assert tenant_transaction_state(db,tenant)==before

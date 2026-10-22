"""Two-target real settlement schedules, disposable SQL metadata only.

Apply decisions are real; terminal child metadata isolates settlement and does
not qualify canonical processing. No triggers or functions are replaced.
"""
from concurrent.futures import ThreadPoolExecutor
from threading import Event
from time import monotonic
from uuid import uuid4
import psycopg
import pytest
from semantic_mixed_retouch_fixture import mixed_decision_fixture, commit_mixed_decision
from test_v27_semantic_target_lifecycle import settle
from removal_transaction_state import tenant_transaction_state


def ready(db, outcome):
    f = mixed_decision_fixture(db, approved_count=2)
    commit_mixed_decision(db, f)
    tenant, child = f['args'][0], f['child']
    db.execute('UPDATE photo.runs SET status=%s WHERE tenant_id=%s AND id=%s', (outcome, tenant, child))
    return f, tenant, child


@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
@pytest.mark.parametrize('release', ['commit', 'rollback'])
def test_same_child_waiter_settles_only_uncommitted_targets(database, outcome, release):
    with psycopg.connect(**database) as seed:
        f, tenant, child = ready(seed, outcome)
        revisions = dict(seed.execute('SELECT id,revision FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s',
                                     (tenant, child)).fetchall())
        assert len(revisions) == 2
    with psycopg.connect(**database) as holder, psycopg.connect(**database) as waiter, \
            psycopg.connect(**database, autocommit=True) as observer:
        baseline = tenant_transaction_state(observer, tenant)
        holder.execute('BEGIN')
        assert settle(holder, tenant, child) == 2
        def second():
            try:
                n = settle(waiter, tenant, child)
                waiter.commit()
                return n
            except BaseException:
                waiter.rollback()
                raise
        with ThreadPoolExecutor(max_workers=1) as pool:
            future = pool.submit(second)
            try:
                deadline = monotonic() + 5
                blocked = False
                while monotonic() < deadline and not future.done():
                    blocked = observer.execute('SELECT %s=ANY(pg_blocking_pids(%s))',
                        (holder.info.backend_pid, waiter.info.backend_pid)).fetchone()[0]
                    if blocked: break
                    Event().wait(.01)
                assert blocked, 'waiter never reached run lock'
                assert tenant_transaction_state(observer, tenant) == baseline
                if release == 'commit': holder.commit()
                else: holder.rollback()
                assert future.result(timeout=10) == (0 if release == 'commit' else 2)
            finally:
                holder.rollback()
                if not future.done(): waiter.cancel()
        target_state = 'applied' if outcome == 'completed' else outcome
        assert observer.execute('SELECT id,state,revision FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s ORDER BY id',
                                (tenant, child)).fetchall() == [(target, target_state, revision+1) for target, revision in sorted(revisions.items())]
        after = tenant_transaction_state(observer, tenant)
        assert settle(holder, tenant, child) == 0
        holder.commit()
        assert tenant_transaction_state(observer, tenant) == after


@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
def test_two_target_settlement_caller_rollback_and_scope_refusals(db, outcome):
    f, tenant, child = ready(db, outcome)
    baseline = tenant_transaction_state(db, tenant)
    assert settle(db, uuid4(), child) == 0
    assert settle(db, tenant, uuid4()) == 0
    assert tenant_transaction_state(db, tenant) == baseline
    with db.transaction(force_rollback=True):
        assert settle(db, tenant, child) == 2
        assert tenant_transaction_state(db, tenant) != baseline
    assert tenant_transaction_state(db, tenant) == baseline
    assert settle(db, tenant, child) == 2
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE id=%s', (f['rejected'][0],)).fetchone() == ('rejected',)
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE id=%s', (f['omitted'][0],)).fetchone() == ('proposed',)

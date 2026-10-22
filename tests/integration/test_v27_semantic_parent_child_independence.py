"""Explicit parent close remains independent of child target settlement.

Real mixed retouch admission/reject/close SQL; terminal child metadata isolates
parent/target semantics, not canonical processing or physical output acceptance.
"""
import pytest
from semantic_mixed_retouch_fixture import mixed_decision_fixture, commit_mixed_decision
from semantic_rejection_fixture import rejection_fixture, commit_rejection
from semantic_close_fixture import close_fixture, commit_close, replay_close
from test_v27_semantic_target_lifecycle import settle
from removal_transaction_state import tenant_transaction_state


def parent(db, tenant, run):
    return db.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE tenant_id=%s AND id=%s',
                      (tenant, run)).fetchone()[0]


@pytest.mark.parametrize('outcome', ['completed', 'failed', 'cancelled'])
@pytest.mark.parametrize('close_first', [False, True])
def test_child_settlement_preserves_parent_and_exact_explicit_close_replay(db, outcome, close_first):
    mixed = mixed_decision_fixture(db, approved_count=2)
    applied = commit_mixed_decision(db, mixed)
    remaining = rejection_fixture(db, reviews=mixed['reviews'][-1:], selected_count=1,
                                  parent_revision=int(applied['revision']))
    rejected = commit_rejection(db, remaining)
    f = close_fixture(db, decided=(remaining, rejected))
    tenant, parent_id, child = f['args'][0], f['parent'], mixed['child']
    before_close = parent(db, tenant, parent_id)
    assert before_close['status'] == 'waiting_review'
    close_result = None
    if close_first:
        close_result = commit_close(db, f)
    before_settlement = parent(db, tenant, parent_id)
    # Deliberately isolated metadata transition; canonical child processing
    # has independent coverage and is not asserted by this fixture.
    db.execute('UPDATE photo.runs SET status=%s WHERE tenant_id=%s AND id=%s',
               (outcome, tenant, child))
    assert settle(db, tenant, child) == 2
    assert parent(db, tenant, parent_id) == before_settlement
    expected = 'applied' if outcome == 'completed' else outcome
    assert db.execute('SELECT state FROM photo.semantic_targets WHERE tenant_id=%s AND child_run_id=%s ORDER BY id',
                      (tenant, child)).fetchall() == [(expected,), (expected,)]
    if not close_first:
        assert parent(db, tenant, parent_id)['status'] == 'waiting_review'
        close_result = commit_close(db, f)
    assert close_result['state'] == 'completed'
    assert close_result['revision'] == str(before_close['revision'] + 1)
    assert parent(db, tenant, parent_id)['status'] == 'completed'
    before_replay = tenant_transaction_state(db, tenant)
    assert replay_close(db, f) == ('replayed', close_result)
    assert settle(db, tenant, child) == 0
    assert tenant_transaction_state(db, tenant) == before_replay
    assert db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v27.review.closed'",
                      (tenant, parent_id)).fetchone() == (1,)
    assert db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='v27.review.closed' AND payload->>'run_id'=%s",
                      (tenant, str(parent_id))).fetchone() == (1,)

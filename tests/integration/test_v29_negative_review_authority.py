"""Negative-review SQL privilege and finalization barriers; not full lifecycle qualification."""
from uuid import uuid4
import psycopg
import pytest

CREATE = 'photo.create_v29_negative_review(uuid,uuid,uuid,uuid,uuid,text)'
PENDING = 'photo.pending_v29_negative_reviews(uuid)'


def test_negative_review_entrypoints_are_scheduler_only(db):
    for signature in (CREATE, PENDING):
        assert db.execute('SELECT has_function_privilege(%s,%s,%s)',
            ('photo_scheduler',signature,'EXECUTE')).fetchone()[0]
        for role in ('photo_api','photo_worker','photo_gateway','photo_portal','public'):
            assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',
                (role,signature,'EXECUTE')).fetchone()[0]


def test_negative_review_creation_refuses_unbound_params_without_side_effects(db):
    tenant=uuid4()
    with db.transaction(force_rollback=True):
        db.execute('SET LOCAL ROLE photo_scheduler')
        result=db.execute('SELECT photo.create_v29_negative_review(%s,%s,%s,%s,%s,%s)',
            (tenant,uuid4(),uuid4(),uuid4(),uuid4(),'{}')).fetchone()[0]
        assert result is None
        db.execute('RESET ROLE')
        assert db.execute('SELECT count(*) FROM photo.reviews WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0
        assert db.execute('SELECT count(*) FROM photo.outbox WHERE tenant_id=%s',(tenant,)).fetchone()[0]==0


@pytest.mark.parametrize('role',['photo_api','photo_worker'])
def test_negative_review_rejects_untrusted_publication(db,role):
    with pytest.raises(psycopg.errors.InsufficientPrivilege):
        with db.transaction(force_rollback=True):
            db.execute('SET LOCAL ROLE '+role)
            db.execute('SELECT photo.create_v29_negative_review(%s,%s,%s,%s,%s,%s)',
                (*[uuid4() for _ in range(5)],'{}'))


def test_negative_review_completion_retains_predecessor_and_exact_acceptance(db):
    finalizer=db.execute("SELECT pg_get_functiondef('photo.finalize_v2_run(uuid,uuid)'::regprocedure)").fetchone()[0]
    pending=db.execute("SELECT pg_get_functiondef('photo.v2_pending_finalizations()'::regprocedure)").fetchone()[0]
    for marker in ('finalize_v2_run_pre_render_reviews','optical.negative','v.kind=task.operation',
                   "v.decision='accept'","v.decision_route='candidate'",'run_row.cancel_requested',
                   'run_row.graph_sealed','v.decided_by IS NOT NULL','v.decision_hash IS NOT NULL'):
        assert marker in finalizer
    for marker in ('optical.negative','rv.kind=rt.operation',"rv.decision='accept'",
                   "rv.decision_route='candidate'",'rv.decided_by IS NOT NULL'):
        assert marker in pending

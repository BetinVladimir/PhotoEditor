"""Publication/replay/rollback SQL scenarios, authored and not executed."""
import pytest
from psycopg import errors
from relight_review_publication_fixture import relight_publication_fixture, publish_relight_review


def counts(db, f):
    return (
        db.execute('SELECT count(*) FROM photo.reviews WHERE tenant_id=%s AND task_id=%s', (f['tenant'],f['task'])).fetchone()[0],
        db.execute("SELECT count(*) FROM photo.run_events WHERE tenant_id=%s AND run_id=%s AND type='v2.run.waiting_review'", (f['tenant'],f['run'])).fetchone()[0],
        db.execute("SELECT count(*) FROM photo.outbox WHERE tenant_id=%s AND routing_key='review.created' AND payload->>'run_id'=%s", (f['tenant'],str(f['run']))).fetchone()[0],
    )


def test_relight_publication_and_exact_replay_do_not_duplicate_events(db):
    f = relight_publication_fixture(db)
    first = publish_relight_review(db, f)
    assert first is not None
    assert counts(db, f) == (1, 1, 1)
    assert db.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s', (f['tenant'],f['run'])).fetchone()[0] == 'waiting_review'
    assert publish_relight_review(db, f) == first
    assert counts(db, f) == (1, 1, 1)
    assert publish_relight_review(db, f, f['body'] + ' ') is None
    assert counts(db, f) == (1, 1, 1)


@pytest.mark.parametrize('case', ['cancelled', 'stale_fence', 'expired_source', 'missing_marker', 'params_bytes', 'params_version'])
def test_relight_publication_refuses_stale_evidence_without_writes(db, case):
    if case in ('params_bytes','params_version'):
        # Each constructor installs global qualification/admission policy rows.
        # Finish both immutable refusal and valid publication in one owned
        # savepoint, then discard that entire fixture before constructing another.
        with db.transaction(force_rollback=True):
            f = relight_publication_fixture(db)
            # Persisted content identity cannot be mutated, even by fixture owner.
            field='bytes' if case=='params_bytes' else 'object_version'
            with pytest.raises(errors.CheckViolation,match='immutable field artifacts.'+field):
                with db.transaction():
                    if case=='params_bytes':
                        db.execute('UPDATE photo.artifacts SET bytes=bytes+1 WHERE tenant_id=%s AND id=%s',(f['tenant'],f['params']))
                    else:
                        db.execute("UPDATE photo.artifacts SET object_version='null' WHERE tenant_id=%s AND id=%s",(f['tenant'],f['params']))
            assert counts(db,f)==(0,0,0)
            # Identical helper construction with correct metadata remains valid.
            assert publish_relight_review(db,f) is not None
            assert counts(db,f)==(1,1,1)
        # Savepoint rollback restores both global policy rows and SET LOCAL ROLE;
        # no policy deletion, constraint change or outer transaction rollback.
        assert db.execute('SELECT current_user=session_user').fetchone()==(True,)
        malformed=relight_publication_fixture(db,
            params_declared_bytes_delta=1 if case=='params_bytes' else 0,
            params_declared_version='null' if case=='params_version' else None)
        assert publish_relight_review(db,malformed) is None
        assert counts(db,malformed)==(0,0,0)
        return
    f = relight_publication_fixture(db)
    if case == 'cancelled':
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE tenant_id=%s AND id=%s', (f['tenant'],f['task']))
    elif case == 'stale_fence':
        db.execute('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE tenant_id=%s AND id=%s', (f['tenant'],f['task']))
    elif case == 'expired_source':
        db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s", (f['tenant'],f['base']))
    else:
        db.execute("DELETE FROM photo.inbox WHERE tenant_id=%s AND consumer='v2-adopt'", (f['tenant'],))
    assert publish_relight_review(db, f) is None
    assert counts(db, f) == (0, 0, 0)


def test_relight_publication_is_part_of_its_outer_transaction(db):
    f = relight_publication_fixture(db)
    class AbortPublication(Exception):
        pass
    with pytest.raises(AbortPublication):
        with db.transaction():
            assert publish_relight_review(db, f) is not None
            assert counts(db, f) == (1, 1, 1)
            raise AbortPublication()
    assert counts(db, f) == (0, 0, 0)
    assert db.execute('SELECT status FROM photo.runs WHERE tenant_id=%s AND id=%s', (f['tenant'],f['run'])).fetchone()[0] == 'running'

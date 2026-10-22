"""SQL wrapper scenarios with a controlled delegate; not a full task lifecycle proof.

Replace only the private predecessor inside the rollback fixture to expose the
final fence independently of operation-specific input/quality prerequisites.
"""
import pytest
from psycopg import errors
from psycopg.types.json import Jsonb
from test_task_leases import queued_task


def controlled_claim(db, *, enabled=True, outcome='claimed', backend='cpu'):
    item = queued_task(db)
    db.execute("UPDATE photo.tasks SET operation='image.relight',operation_version='1',resource_class='cpu',execution_profile=%s WHERE id=%s",
        (Jsonb({'backend':backend}),item['task']))
    db.execute("UPDATE photo.operation_registry SET v2_enabled=%s WHERE operation='image.relight' AND version='1'",(enabled,))
    # Use a fixed return type identical to the predecessor; mutation is a rollback witness.
    db.execute("""CREATE OR REPLACE FUNCTION photo.claim_task_before_v2_activation_guard(p_task uuid,p_attempt integer,p_worker uuid,p_command jsonb)
    RETURNS TABLE(outcome text,fence bigint,lease_until timestamptz,deadline timestamptz,context jsonb)
    LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
    BEGIN
     UPDATE photo.tasks SET attempt=attempt+1 WHERE id=$1;
     RETURN QUERY SELECT $4->>'outcome',7::bigint,NULL::timestamptz,NULL::timestamptz,'{}'::jsonb;
    END $$""")
    return item, {'outcome':outcome}


def test_disabled_v2_apply_operation_rolls_back_delegated_claim(db):
    item, command = controlled_claim(db,enabled=False)
    before = db.execute('SELECT attempt FROM photo.tasks WHERE id=%s',(item['task'],)).fetchone()
    with pytest.raises(errors.SerializationFailure,match='ACTIVATION_STALE'):
        with db.transaction():
            db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(item['task'],item['worker'],Jsonb(command)))
    assert db.execute('SELECT attempt FROM photo.tasks WHERE id=%s',(item['task'],)).fetchone() == before


def test_enabled_matching_backend_keeps_delegate_result(db):
    item, command = controlled_claim(db)
    assert db.execute('SELECT outcome,fence FROM photo.claim_task(%s,1,%s,%s)',
        (item['task'],item['worker'],Jsonb(command))).fetchone() == ('claimed',7)


def test_registry_backend_revocation_rolls_back_claim(db):
    item, command = controlled_claim(db,backend='cuda')
    db.execute("UPDATE photo.operation_registry SET backends='[\"cpu\"]'::jsonb WHERE operation='image.relight' AND version='1'")
    with pytest.raises(errors.SerializationFailure,match='ACTIVATION_STALE'):
        with db.transaction():
            db.execute('SELECT * FROM photo.claim_task(%s,1,%s,%s)',(item['task'],item['worker'],Jsonb(command)))


@pytest.mark.parametrize('outcome',['completed','busy','stale','unavailable'])
def test_non_claimed_replay_outcomes_are_preserved_when_operation_disabled(db,outcome):
    item, command = controlled_claim(db,enabled=False,outcome=outcome)
    assert db.execute('SELECT outcome FROM photo.claim_task(%s,1,%s,%s)',
        (item['task'],item['worker'],Jsonb(command))).fetchone() == (outcome,)


def test_worker_cannot_bypass_final_fence_through_private_predecessor(db):
    assert db.execute("SELECT has_function_privilege('photo_worker',"
        "'photo.claim_task_before_v2_activation_guard(uuid,integer,uuid,jsonb)','EXECUTE')").fetchone() == (False,)

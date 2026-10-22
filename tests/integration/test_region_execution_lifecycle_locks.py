"""Real committed canonical producers and independent connections; authored only."""
from copy import deepcopy
import psycopg
import pytest
from psycopg.types.json import Jsonb
from test_region_execution_provenance import execution_fixture


def locked(connection,tenant,actor,session,plan):
    connection.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
    connection.execute('SET LOCAL ROLE photo_scheduler')
    try:
        return connection.execute('SELECT photo.v26_region_locked_scope(%s,%s,%s,%s)',
            (tenant,actor,session,Jsonb(plan))).fetchone()[0]
    finally:
        # On an aborted lock-timeout transaction, its context restores role.
        if connection.info.transaction_status != psycopg.pq.TransactionStatus.INERROR:
            connection.execute('RESET ROLE')


def test_whole_scope_positive_then_live_execution_lifecycle_refusal(db):
    tenant,task,run,output,session,plan,_=execution_fixture(db)
    actor=db.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    with db.transaction():assert locked(db,tenant,actor,session,plan) is True
    mutations=[
        ('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',task),
        ('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',run),
        ('UPDATE photo.tasks SET attempt=attempt+1 WHERE id=%s',task),
        ('UPDATE photo.tasks SET fencing_token=fencing_token+1 WHERE id=%s',task),
        ("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",output),
        ('UPDATE photo.sessions SET revoked_at=now() WHERE id=%s',session),
    ]
    for query,identity in mutations:
        with db.transaction(force_rollback=True):
            db.execute(query,(identity,))
            assert locked(db,tenant,actor,session,plan) is False
    changed=deepcopy(plan);changed['source']['object_version']='other-version'
    with db.transaction():assert locked(db,tenant,actor,session,changed) is False


@pytest.mark.parametrize('kind',['run_cancel','task_cancel','artifact_expiry'])
def test_locked_scope_serializes_current_producer_mutation_and_rechecks_after_commit(database,kind):
    with psycopg.connect(**database,autocommit=True) as setup:
        with setup.transaction():
            tenant,task,run,output,session,plan,_=execution_fixture(setup)
            actor=setup.execute('SELECT created_by FROM photo.runs WHERE id=%s',(run,)).fetchone()[0]
    query,identity={
        'run_cancel':('UPDATE photo.runs SET cancel_requested=true WHERE id=%s',run),
        'task_cancel':('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',task),
        'artifact_expiry':("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",output),
    }[kind]
    with psycopg.connect(**database,autocommit=True) as reader,psycopg.connect(**database,autocommit=True) as writer:
        # Positive admission holds actual upstream locks through transaction end.
        with reader.transaction():
            assert locked(reader,tenant,actor,session,plan) is True
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with writer.transaction():
                    writer.execute("SET LOCAL lock_timeout='150ms'")
                    writer.execute(query,(identity,))
        # Opposite interleaving cannot read past an uncommitted invalidation.
        with writer.transaction():
            writer.execute(query,(identity,))
            with pytest.raises(psycopg.errors.LockNotAvailable):
                with reader.transaction():
                    reader.execute("SET LOCAL lock_timeout='150ms'")
                    locked(reader,tenant,actor,session,plan)
        with reader.transaction():assert locked(reader,tenant,actor,session,plan) is False


def test_review_noop_authority_is_repeated_by_whole_locked_scope(db):
    from test_region_reviewed_authority import base_fixture
    tenant,producer,proposal,plan,_=base_fixture(db)
    stage=plan['execution']['stages'][0];run=stage['task_id']
    actor=db.execute('SELECT r.created_by FROM photo.tasks t JOIN photo.runs r ON r.id=t.run_id AND r.tenant_id=t.tenant_id WHERE t.id=%s',(run,)).fetchone()[0]
    # Session identity is part of the accepted source proof, not review payload.
    session=db.execute('SELECT id FROM photo.sessions WHERE user_id=%s AND revoked_at IS NULL ORDER BY id LIMIT 1',(actor,)).fetchone()[0]
    with db.transaction():assert locked(db,tenant,actor,session,plan) is True
    with db.transaction(force_rollback=True):
        db.execute('UPDATE photo.tasks SET cancel_requested=true WHERE id=%s',(producer,))
        assert locked(db,tenant,actor,session,plan) is False
    with db.transaction(force_rollback=True):
        db.execute("UPDATE photo.artifacts SET expires_at=now()-interval '1 second' WHERE id=%s",(proposal['artifact_id'],))
        assert locked(db,tenant,actor,session,plan) is False

"""Close locks retained to caller commit, using the disposable DB fixture."""
import psycopg
import pytest
from semantic_close_fixture import close_fixture, commit_close


@pytest.mark.parametrize('locked',['parent','first_target','second_target','publication','receipt'])
def test_close_holds_existing_authority_rows_until_transaction_end(database,locked):
    with psycopg.connect(**database) as seed:
        f=close_fixture(seed)
        args=f['args']
        receipt=seed.execute('INSERT INTO photo.idempotency_records(tenant_id,principal_type,principal_id,route,key,request_hash,expires_at) '
            "VALUES(%s,'user',%s,'SemanticIntent.CloseReview',%s,%s,clock_timestamp()+interval '1 day') RETURNING id",
            (args[0],args[2],f['key'],f['request_sha'])).fetchone()[0]
        targets=sorted(f['rejected']['selected'])
    rows=dict(parent=('runs',f['parent']),first_target=('semantic_targets',targets[0]),
        second_target=('semantic_targets',targets[1]),publication=('artifacts',f['artifact']),
        receipt=('idempotency_records',receipt))
    table,identifier=rows[locked]
    # Table identifiers come only from the explicit allowlist above.
    query='SELECT id FROM photo.'+table+' WHERE id=%s FOR UPDATE NOWAIT'
    with psycopg.connect(**database) as holder,psycopg.connect(**database) as writer:
        holder.execute('BEGIN')
        assert commit_close(holder,f,reserve_receipt=False)['state']=='completed'
        with pytest.raises(psycopg.errors.LockNotAvailable):
            with writer.transaction(): writer.execute(query,(identifier,)).fetchone()
        holder.rollback()
        with writer.transaction(force_rollback=True):
            assert writer.execute(query,(identifier,)).fetchone()==(identifier,)

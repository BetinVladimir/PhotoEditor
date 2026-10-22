"""Forward upgrade preserves receipts/counters; settlement requires a cleanup call."""
from pathlib import Path
from uuid import uuid4
import pytest
from test_gateway_outputs import setup,begin,complete

ROOT=Path(__file__).resolve().parents[2]


@pytest.mark.migration_head(2538)
def test_gc_upgrade_preserves_existing_charge_and_receipt_then_settles_once(db):
    case=setup(db)
    db.execute('SET LOCAL ROLE photo_gateway')
    receipt=begin(db,case,uuid4())
    assert receipt[0]=='created'
    assert complete(db,case,receipt[1])[0]=='committed'
    db.execute('RESET ROLE')
    token=uuid4()
    db.execute("UPDATE photo.artifacts SET state='deleting',gc_token=%s,gc_lease_until=clock_timestamp()+interval '1 hour' WHERE id=%s",(token,receipt[2]))
    artifact=db.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE id=%s',(receipt[2],)).fetchone()[0]
    upload=db.execute('SELECT to_jsonb(u) FROM photo.output_uploads u WHERE id=%s',(receipt[1],)).fetchone()[0]
    usage=db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()
    signatures=['photo.claim_artifact_gc()','photo.finish_artifact_gc(uuid,uuid)','photo.finish_original_cleanup(uuid,uuid)','photo.finish_staging_cleanup(uuid,uuid)']
    def authority(signature):
        return db.execute('SELECT oid,proowner,proacl::text,prosecdef FROM pg_proc WHERE oid=%s::regprocedure',(signature,)).fetchone()
    previous={signature:authority(signature) for signature in signatures}
    migration=(ROOT/'backend/db/migrations/02539_artifact_gc_storage_settlement.sql').read_text().split('-- +goose Down')[0]
    db.execute(migration)
    after=db.execute('SELECT to_jsonb(a) FROM photo.artifacts a WHERE id=%s',(receipt[2],)).fetchone()[0]
    assert after.pop('gc_storage_settled') is False
    assert after==artifact
    assert db.execute('SELECT to_jsonb(u) FROM photo.output_uploads u WHERE id=%s',(receipt[1],)).fetchone()[0]==upload
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==usage
    for signature in signatures:
        assert authority(signature)==previous[signature]
    db.execute('SET LOCAL ROLE photo_maintenance')
    assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(receipt[2],token)).fetchone()==(True,)
    assert db.execute('SELECT photo.finish_artifact_gc(%s,%s)',(receipt[2],token)).fetchone()==(True,)
    db.execute('RESET ROLE')
    assert db.execute('SELECT storage_bytes,reserved_bytes FROM photo.tenant_usage WHERE tenant_id=%s',(case['tenant'],)).fetchone()==(usage[0]-2,usage[1])

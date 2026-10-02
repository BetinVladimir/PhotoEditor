from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00100_artifact_gc.sql').read_text()
def test_gc_claim_is_expired_unreferenced_fenced_and_private():
    for value in ("state='ready'","expires_at<=clock_timestamp()","artifact_leases","photo.assets","photo.asset_versions","photo.derivatives","photo.exports","photo.download_volumes","FOR UPDATE SKIP LOCKED","state='deleting'","gc_lease_until","GRANT EXECUTE ON FUNCTION photo.claim_artifact_gc() TO photo_maintenance"):
        assert value in SQL
def test_gc_finish_and_recovery_are_token_fenced():
    assert "a.gc_token IS DISTINCT FROM p_token" in SQL
    assert "photo.artifact_has_references(a.tenant_id,a.id)" in SQL
    assert "UPDATE photo.artifacts SET state='deleted'" in SQL
    assert "CREATE FUNCTION photo.recover_artifact_gc()" in SQL
    assert "state='ready',gc_token=NULL,gc_lease_until=NULL" in SQL

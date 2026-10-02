from pathlib import Path

SQL=(Path(__file__).resolve().parents[2]/'backend/db/migrations/00154_tenant_deletion_cleanup.sql').read_text()
def test_tenant_cleanup_is_leased_fenced_and_waits_for_runs():
 assert "CREATE FUNCTION photo.claim_tenant_deletion" in SQL
 assert "cleanup_lease_until<=clock_timestamp()" in SQL
 assert "FOR UPDATE OF x SKIP LOCKED" in SQL
 assert "r.status IN('draft','queued','running','waiting_review','cancelling')" in SQL
 assert "cleanup_token=p_token AND cleanup_lease_until>clock_timestamp()" in SQL
 assert "GRANT EXECUTE ON FUNCTION photo.finish_tenant_deletion(uuid,uuid) TO photo_maintenance" in SQL

def test_finish_keeps_an_inaccessible_tombstone():
 assert "UPDATE photo.memberships SET status='suspended'" in SQL
 assert "UPDATE photo.tenants SET status='deleted'" in SQL
 assert "UPDATE photo.deletion_jobs SET status='completed'" in SQL

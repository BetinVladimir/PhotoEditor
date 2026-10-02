from pathlib import Path


ROOT = Path(__file__).resolve().parents[2]
SQL = (ROOT / "backend/db/migrations/00097_tenant_lifecycle.sql").read_text()


def test_tenant_creation_is_privileged_atomic_and_idempotent():
    for required in (
        "CREATE FUNCTION photo.create_tenant",
        "SECURITY DEFINER SET search_path=pg_catalog,photo",
        "JOIN photo.users u ON u.id=s.user_id",
        "u.status='active'",
        "INSERT INTO photo.auth_idempotency_records",
        "IF idem.request_hash<>p_request_hash",
        "IF idem.response<>'{}'::jsonb",
        "INSERT INTO photo.tenants",
        "INSERT INTO photo.memberships(tenant_id,user_id,role,status) VALUES(t.id,p_user,'owner','active')",
        "INSERT INTO photo.tenant_usage(tenant_id) VALUES(t.id)",
        "'tenant.create'",
        "GRANT EXECUTE ON FUNCTION photo.create_tenant(uuid,uuid,uuid,text,jsonb,jsonb,text,text) TO photo_api",
    ):
        assert required in SQL


def test_tenant_deletion_request_is_owner_only_fenced_and_replayable():
    for required in (
        "CREATE FUNCTION photo.request_tenant_deletion",
        "m.role='owner' AND m.status='active'",
        "WHERE x.id=p_tenant FOR UPDATE OF x",
        "IF t.status='deleting'",
        "IF t.revision<>p_expected",
        "UPDATE photo.tenants SET status='deleting',revision=revision+1",
        "UPDATE photo.share_links SET revoked_at=coalesce(revoked_at,clock_timestamp())",
        "UPDATE photo.share_sessions SET revoked_at=coalesce(revoked_at,clock_timestamp())",
        "UPDATE photo.runs SET cancel_requested=true",
        "INSERT INTO photo.deletion_jobs",
        "'tenant.delete_requested'",
        "GRANT EXECUTE ON FUNCTION photo.request_tenant_deletion(uuid,uuid,uuid,bigint,uuid) TO photo_api",
    ):
        assert required in SQL


def test_tenant_lifecycle_functions_are_not_public():
    assert "REVOKE ALL ON FUNCTION photo.create_tenant" in SQL
    assert "REVOKE ALL ON FUNCTION photo.request_tenant_deletion" in SQL

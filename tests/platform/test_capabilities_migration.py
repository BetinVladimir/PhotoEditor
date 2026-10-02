from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00114_tenant_capabilities.sql').read_text()

def test_capabilities_are_tenant_scoped_fresh_and_backend_qualified():
    for required in (
        "CREATE FUNCTION photo.tenant_capabilities()",
        "SECURITY DEFINER STABLE SET search_path=pg_catalog,photo",
        "current_setting('app.tenant_id',true)",
        "w.tenant_id=t.tenant_id AND w.enabled",
        "w.last_seen>current_timestamp-interval '30 seconds'",
        "n.enabled AND n.reported_at>current_timestamp-interval '30 seconds'",
        "jsonb_object_keys(coalesce(w.qualified_operations->op.operation",
        "w.backends ? backend.value AND op.backends ? backend.value",
    ):
        assert required in SQL

def test_capability_function_is_private_and_api_callable():
    assert "REVOKE ALL ON FUNCTION photo.tenant_capabilities() FROM PUBLIC" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.tenant_capabilities() TO photo_api" in SQL

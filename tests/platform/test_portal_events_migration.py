from pathlib import Path
ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00115_portal_events.sql').read_text()

def test_portal_events_reauthorize_filter_and_expire_cursor():
    for required in (
        "photo.authorize_portal_session(p_session_hash,NULL,'session')",
        "e.tenant_id=ident.tenant_id AND e.collection_id=ident.collection_id",
        "e.type IN ('collection_changed','upload_status','download_status')",
        "e.principal_id IS NULL OR e.principal_id=ident.session_id",
        "p_after<minimum-1",
        "'expired'",
        "ORDER BY e.cursor LIMIT p_limit",
    ): assert required in SQL

def test_portal_event_function_is_private():
    assert "SECURITY DEFINER STABLE SET search_path=pg_catalog,photo" in SQL
    assert "REVOKE ALL ON FUNCTION photo.list_portal_events" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.list_portal_events(bytea,bigint,integer) TO photo_portal" in SQL

from pathlib import Path
SQL=(Path(__file__).parents[2]/'backend/db/migrations/00168_blind_download_projection.sql').read_text()

def test_download_capability_is_independent_while_catalog_remains_view_gated():
    assert "'download',l.permissions->'download'='true'::jsonb" in SQL
    assert "CASE WHEN can_view THEN coalesce" in SQL
    assert "CASE WHEN p_scope='all' THEN 'download' ELSE 'view' END" in SQL
    assert "p_scope='all' AND l.permissions->'download'<>'true'::jsonb" in SQL
    assert "p_scope<>'all' AND l.permissions->'view'<>'true'::jsonb" in SQL
    assert 'CREATE EXTENSION IF NOT EXISTS pgcrypto' in SQL and SQL.count('public.digest(')==3

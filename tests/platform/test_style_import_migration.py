from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
SQL=(ROOT/'backend/db/migrations/00160_style_import.sql').read_text()

def test_style_import_is_real_queued_operation():
    for token in ('style.import','claim_style_import_build','reserve_style_import_document','materialize_style_import_task','style_asset'):
        assert token in SQL
    assert "t.operation IN('style.aggregate','style.import')" in SQL

def test_import_tables_are_tenant_isolated():
    assert 'ALTER TABLE photo.style_import_builds FORCE ROW LEVEL SECURITY' in SQL
    assert 'CREATE POLICY tenant_scope ON photo.style_import_builds' in SQL

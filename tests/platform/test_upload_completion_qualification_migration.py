from pathlib import Path


SQL = (Path(__file__).resolve().parents[2] / "backend/db/migrations/00169_upload_completion_qualification.sql").read_text()


def test_upload_completion_usage_lookup_is_qualified():
    assert "FROM photo.tenant_usage usage WHERE usage.tenant_id=hint.tenant_id FOR UPDATE" in SQL
    assert "GRANT EXECUTE ON FUNCTION photo.claim_upload_completion() TO photo_scheduler" in SQL


def test_upload_completion_fix_is_forward_only_and_goose_safe():
    assert SQL.count("-- +goose StatementBegin") == SQL.count("-- +goose StatementEnd") == 2
    assert "RAISE EXCEPTION 'Forward-only migration'" in SQL

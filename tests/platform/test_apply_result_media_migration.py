from pathlib import Path


SQL = (Path(__file__).resolve().parents[2] / "backend/db/migrations/00156_apply_result_media.sql").read_text()


def test_apply_acceptance_enqueues_exact_media_origin():
    assert "origin IN ('ingest','apply')" in SQL
    assert "AFTER UPDATE OF review_state ON photo.asset_versions" in SQL
    assert "OLD.review_state='accepted'" in SQL
    assert "VALUES(gen_random_uuid(),NEW.tenant_id,NEW.id" in SQL
    assert "'[\"thumbnail\",\"preview\"]'" in SQL
    assert "ON CONFLICT(tenant_id,origin,origin_run_id,asset_version_id)" in SQL


def test_apply_media_claim_revalidates_provenance():
    assert "r.kind=j.origin" in SQL
    assert "v.created_by_run_id=r.id" in SQL
    assert "v.review_state='accepted'" in SQL
    assert "x.upload_state='verified'" in SQL
    assert "x.deleted_at IS NULL" in SQL
    assert "source.kind<>j.origin" in SQL

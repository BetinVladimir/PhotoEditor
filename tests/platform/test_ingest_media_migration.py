from pathlib import Path

def test_ingest_media_jobs_preserve_distinct_provenance_and_auto_enqueue():
 sql=(Path(__file__).resolve().parents[2]/'backend/db/migrations/00153_ingest_media_derivatives.sql').read_text()
 assert "origin='bulk_repair'" in sql and "origin='ingest'" in sql
 assert "bulk_operation_id IS NULL AND item_id IS NULL AND origin_run_id IS NOT NULL" in sql
 assert "CREATE TRIGGER enqueue_ingest_media AFTER INSERT ON photo.asset_versions" in sql
 assert "NEW.review_state<>'accepted'" in sql and "kind_ref='ingest'" in sql
 assert "x.upload_state='verified'" in sql and "x.deleted_at IS NULL" in sql
 assert "coalesce(j.bulk_operation_id,'00000000-0000-0000-0000-000000000000'::uuid)" in sql
 assert "IF origin_ref='bulk_repair' THEN RETURN photo.finalize_media_retry_job_bulk_v1();" in sql
 assert "REVOKE ALL ON FUNCTION photo.finalize_media_retry_job_bulk_v1() FROM PUBLIC,photo_scheduler" in sql

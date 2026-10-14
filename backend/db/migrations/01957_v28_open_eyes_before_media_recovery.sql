-- +goose Up
-- Recover reviews created before the donor before-media hook became active.
-- One bounded candidate per scheduler pass; the existing enqueue function
-- rechecks the accepted source under tenant authority and unique job identity.
-- +goose StatementBegin
CREATE FUNCTION photo.enqueue_next_v28_open_eyes_before_media()
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE selected record;event_id uuid;
BEGIN
 SELECT review.tenant_id,review.id INTO selected FROM photo.reviews review
 JOIN photo.tenants tenant ON tenant.id=review.tenant_id AND tenant.status='active'
 JOIN photo.runs parent ON parent.tenant_id=review.tenant_id AND parent.id=review.run_id
 JOIN photo.v2_workflows workflow ON workflow.tenant_id=parent.tenant_id AND workflow.run_id=parent.id
 JOIN photo.asset_versions version ON version.tenant_id=review.tenant_id
  AND version.id=CASE WHEN parent.v28_donor_descriptor_jcs::jsonb->'params'->'base'->>'asset_version_id'
   ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$'
   THEN (parent.v28_donor_descriptor_jcs::jsonb->'params'->'base'->>'asset_version_id')::uuid END
  AND version.master_artifact_id=review.base_artifact_id
  AND version.content_hash=parent.v28_donor_descriptor_jcs::jsonb->'params'->'base'->'source'->>'sha256'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
 WHERE review.kind='portrait.open_eyes' AND review.state='pending'
  AND parent.kind='v2' AND parent.status='waiting_review' AND parent.graph_sealed
  AND NOT parent.cancel_requested AND version.review_state='accepted'
  AND version.created_by_run_id IS NOT NULL
  AND asset.project_id=workflow.project_id AND asset.deleted_at IS NULL
  AND asset.upload_state='verified'
  AND NOT EXISTS(SELECT 1 FROM photo.media_retry_jobs job
   WHERE job.tenant_id=review.tenant_id AND job.review_id=review.id
    AND job.asset_version_id=version.id AND job.origin='review')
 ORDER BY review.created_at,review.id LIMIT 1;
 IF NOT FOUND THEN RETURN false;END IF;
 event_id:=photo.enqueue_v26_review_before_media(selected.tenant_id,selected.id);
 RETURN event_id IS NOT NULL;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.enqueue_next_v28_open_eyes_before_media() OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.enqueue_next_v28_open_eyes_before_media() FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.enqueue_next_v28_open_eyes_before_media() TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

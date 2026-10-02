-- +goose Up
-- Resolve only the version accepted by the immutable receipt. No arbitrary
-- artifact lookup and no device credential is used by the scheduler.
-- +goose StatementBegin
CREATE FUNCTION photo.capture_assessment_source(p_tenant uuid,p_job uuid,p_token uuid)
RETURNS TABLE(asset_version_id uuid,artifact_id uuid,sha256 text,bytes bigint,
 bucket text,object_key text,object_version text,media_type text,original_sha text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT v.id,a.id,a.sha256,a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,r.content_sha
 FROM photo.capture_assessment_jobs j
 JOIN photo.tenants tenant ON tenant.id=j.tenant_id AND tenant.status='active'
 JOIN photo.capture_receipts r ON r.tenant_id=j.tenant_id AND r.project_id=j.project_id
  AND r.id=j.receipt_id AND r.session_id=j.session_id
 JOIN photo.capture_receipt_assets binding ON binding.tenant_id=j.tenant_id
  AND binding.project_id=j.project_id AND binding.receipt_id=j.receipt_id
  AND binding.asset_version_id=j.asset_version_id
 JOIN photo.asset_versions v ON v.tenant_id=j.tenant_id AND v.id=j.asset_version_id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE j.tenant_id=p_tenant AND j.id=p_job AND j.state='leased'
  AND j.lease_token=p_token AND j.lease_until>clock_timestamp()
  AND a.state='ready' AND a.output_role='working_image' AND a.media_type='application/json'
  AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.capture_assessment_source(uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.capture_assessment_source(uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.capture_assessment_source(uuid,uuid,uuid) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd

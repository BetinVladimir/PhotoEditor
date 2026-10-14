-- +goose Up
-- Explicit version only: never select latest during publication or retry.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_delivery_watermark_source(p_tenant uuid,p_project uuid,p_asset uuid,p_version uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(asset_version_id uuid,artifact_id uuid,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT v.id,v.master_artifact_id,v.content_hash
 FROM photo.assets asset JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
 AND asset.tenant_id=p_tenant AND asset.project_id=p_project AND asset.id=p_asset
 AND asset.deleted_at IS NULL AND asset.upload_state='verified' AND v.id=p_version AND v.review_state='accepted'
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp())
 AND EXISTS(SELECT 1 FROM photo.projects p WHERE p.tenant_id=p_tenant AND p.id=p_project AND p.archived_at IS NULL AND p.download_allowed)
 AND photo.v2_draft_artifact_scope(p_tenant,p_project,p_version,a.id,a.sha256,'WorkingImage',p_actor,p_session,NULL,NULL) IS TRUE;
$$;
ALTER FUNCTION photo.v30_delivery_watermark_source(uuid,uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_delivery_watermark_source(uuid,uuid,uuid,uuid,uuid,uuid) FROM PUBLIC,photo_scheduler,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_delivery_watermark_source(uuid,uuid,uuid,uuid,uuid,uuid) TO photo_api;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

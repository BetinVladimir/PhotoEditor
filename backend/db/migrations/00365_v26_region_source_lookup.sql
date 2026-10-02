-- +goose Up
-- Narrow authorized original-source lookup for RegionPreview compilation.
-- +goose StatementBegin
CREATE FUNCTION photo.v26_region_source(p_tenant uuid,p_asset uuid,p_version uuid,p_actor uuid,p_session uuid)
RETURNS TABLE(project_id uuid,artifact_id uuid,sha256 text,bucket text,object_key text,object_version text,bytes bigint,media_type text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT asset.project_id,a.id,a.sha256,a.bucket,a.object_key,a.object_version,a.bytes,a.media_type
 FROM photo.assets asset JOIN photo.asset_versions v ON v.tenant_id=asset.tenant_id AND v.asset_id=asset.id
 JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.master_artifact_id AND a.sha256=v.content_hash
 WHERE asset.tenant_id=p_tenant AND asset.id=p_asset AND v.id=p_version
 AND asset.deleted_at IS NULL AND v.review_state='accepted'
 AND photo.v2_editor_authorized(p_tenant,asset.project_id,p_actor,p_session)
 AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/json'
 AND a.bytes BETWEEN 2 AND 1048576 AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
 AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v26_region_source(uuid,uuid,uuid,uuid,uuid) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v26_region_source(uuid,uuid,uuid,uuid,uuid) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v26_region_source(uuid,uuid,uuid,uuid,uuid) TO photo_api,photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration'; END $$;
-- +goose StatementEnd

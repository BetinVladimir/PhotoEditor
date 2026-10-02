-- +goose Up
-- Strict pre-publication authority for a new user binary mask. No project-wide
-- producer fallback: the upload must belong to this exact live asset version.
-- +goose StatementBegin
CREATE FUNCTION photo.read_v25_user_mask_pixels(p_tenant uuid,p_version uuid,p_actor uuid,p_session uuid,
 p_artifact uuid,p_sha text,p_width bigint,p_height bigint)
RETURNS TABLE(bytes bigint,bucket text,object_key text,object_version text,media_type text,sha256 text)
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT a.bytes,a.bucket,a.object_key,a.object_version,a.media_type,a.sha256
 FROM photo.v2_draft_uploads u
 JOIN photo.artifacts a ON a.tenant_id=u.tenant_id AND a.id=u.artifact_id
 JOIN photo.asset_versions v ON v.tenant_id=u.tenant_id AND v.id=u.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 WHERE p_tenant=nullif(current_setting('app.tenant_id',true),'')::uuid
  AND u.tenant_id=p_tenant AND u.asset_version_id=p_version
  AND u.project_id=asset.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND v.review_state='accepted' AND NOT EXISTS(SELECT 1 FROM photo.asset_versions newer
   WHERE newer.tenant_id=v.tenant_id AND newer.asset_id=v.asset_id AND newer.version_no>v.version_no)
  AND photo.v2_editor_authorized(p_tenant,u.project_id,p_actor,p_session)
  AND u.artifact_id=p_artifact AND u.sha256=p_sha AND u.upload_contract='MaskPixels'
  AND u.mask_width=p_width AND u.mask_height=p_height
  AND u.state='ready' AND u.completed_at IS NOT NULL
  AND a.state='ready' AND a.verified_at IS NOT NULL AND a.sha256=u.sha256
  AND a.bytes=u.bytes AND a.bucket=u.bucket AND a.object_version=u.object_version
  AND a.object_key='tenants/'||p_tenant||'/v2-drafts/'||p_artifact
  AND a.media_type='application/x-npy' AND a.output_role='mask_pixels'
  AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
  AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp());
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.read_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,text,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.read_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,text,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.read_v25_user_mask_pixels(uuid,uuid,uuid,uuid,uuid,text,bigint,bigint) TO photo_api;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

-- +goose Up
-- Consume the verified publication binding for scheduler input preflight.
-- +goose StatementBegin
CREATE FUNCTION photo.v25_layer_original_user_mask_pixels_scope(p_tenant uuid,p_project uuid,p_snapshot uuid,
 p_actor uuid,p_session uuid,p_params jsonb,p_mask uuid,p_mask_sha text,p_pixels uuid,p_pixels_sha text,
 p_width bigint,p_height bigint) RETURNS boolean
LANGUAGE sql STABLE SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
 SELECT photo.v25_layer_original_user_mask_scope(p_tenant,p_project,p_snapshot,p_actor,p_session,p_params,p_mask,p_mask_sha)
 AND EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
  JOIN photo.v2_draft_uploads pixels_upload ON pixels_upload.tenant_id=root.tenant_id AND pixels_upload.project_id=root.project_id
   AND pixels_upload.asset_version_id=root.asset_version_id
  JOIN photo.artifacts a ON a.tenant_id=pixels_upload.tenant_id AND a.id=pixels_upload.artifact_id
  WHERE root.tenant_id=p_tenant AND root.artifact_id=p_mask AND root.sha256=p_mask_sha
   AND root.user_mask_binding->'source'=p_params->'base'->'source'
   AND root.user_mask_binding->>'canvas_hash'=p_params->'base'->>'canvas_hash'
   AND root.user_mask_binding->'mask'->'artifact'=jsonb_build_object('artifact_id',p_pixels::text,'sha256',p_pixels_sha)
   AND root.user_mask_binding->'mask'->'canvas'->>'width'=p_width::text
   AND root.user_mask_binding->'mask'->'canvas'->>'height'=p_height::text
   AND pixels_upload.artifact_id=p_pixels AND pixels_upload.sha256=p_pixels_sha AND pixels_upload.upload_contract='MaskPixels'
   AND pixels_upload.mask_width=p_width AND pixels_upload.mask_height=p_height AND pixels_upload.state='ready' AND pixels_upload.completed_at IS NOT NULL
   AND a.sha256=pixels_upload.sha256 AND a.bytes=pixels_upload.bytes AND a.bucket=pixels_upload.bucket AND a.bucket=root.bucket
   AND a.object_version=pixels_upload.object_version AND a.object_key='tenants/'||p_tenant||'/v2-drafts/'||p_pixels
   AND a.state='ready' AND a.verified_at IS NOT NULL AND a.media_type='application/x-npy' AND a.output_role='mask_pixels'
   AND a.object_version IS NOT NULL AND a.object_version NOT IN('','null')
   AND (a.expires_at IS NULL OR a.expires_at>clock_timestamp()));
$$;
-- +goose StatementEnd
ALTER FUNCTION photo.v25_layer_original_user_mask_pixels_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,bigint,bigint) OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v25_layer_original_user_mask_pixels_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,bigint,bigint) FROM PUBLIC;
GRANT EXECUTE ON FUNCTION photo.v25_layer_original_user_mask_pixels_scope(uuid,uuid,uuid,uuid,uuid,jsonb,uuid,text,uuid,text,bigint,bigint) TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

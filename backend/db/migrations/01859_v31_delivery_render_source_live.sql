-- +goose Up
-- Live selected source asset/version authority for the frozen render recipe.
-- Descriptor-frame WorkingImages are separate occurrences and are not covered.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_source_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_image uuid,p_image_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;
BEGIN
 IF p_image_sha!~'^[0-9a-f]{64}$' THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.asset_versions v ON v.tenant_id=root.tenant_id
  AND v.id=root.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=v.tenant_id AND asset.id=v.asset_id
 JOIN photo.artifacts image ON image.tenant_id=v.tenant_id
  AND image.id=v.master_artifact_id AND image.sha256=v.content_hash
 WHERE root.tenant_id=(source->>'tenant_id')::uuid
  AND root.project_id=(source->>'project_id')::uuid
  AND root.artifact_id=(e->'params'->>'artifact_id')::uuid
  AND root.sha256=e->'params'->>'sha256'
  AND root.state='ready' AND root.render_registered_at IS NOT NULL
  AND root.render_source_id=p_image AND root.render_source_sha=p_image_sha
  AND asset.project_id=root.project_id AND asset.deleted_at IS NULL
  AND asset.upload_state='verified' AND v.review_state='accepted'
  AND v.master_artifact_id=p_image AND v.content_hash=p_image_sha
  AND image.output_role='working_image' AND image.media_type='application/json'
  AND image.state='ready' AND image.verified_at IS NOT NULL
  AND image.object_version NOT IN('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_source_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_source_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_source_live(uuid,uuid,integer,uuid,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

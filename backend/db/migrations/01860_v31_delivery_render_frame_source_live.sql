-- +goose Up
-- Snapshot-frame WorkingImage authority for a delegated render task. The
-- trusted typed walker derives the frame ordinal/version/ref from descriptor
-- bytes; this SQL guard checks the current snapshot and live asset state.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_frame_source_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_frame_ordinal integer,p_frame_version uuid,
 p_image uuid,p_image_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;
BEGIN
 IF p_frame_ordinal NOT BETWEEN 0 AND 4999 OR
  p_image_sha!~'^[0-9a-f]{64}$' THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL OR e->>'operation'<>'render.compose' OR e->>'version'<>'1'
  THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.session_analyses sa ON sa.tenant_id=root.tenant_id
  AND sa.project_id=root.project_id
  AND sa.id=(root.render_learning->>'analysis_id')::uuid
 JOIN photo.snapshot_items item ON item.tenant_id=sa.tenant_id
  AND item.snapshot_id=sa.snapshot_id
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id
 JOIN photo.artifacts image ON image.tenant_id=version.tenant_id
  AND image.id=version.master_artifact_id AND image.sha256=version.content_hash
 WHERE root.tenant_id=(source->>'tenant_id')::uuid
  AND root.project_id=(source->>'project_id')::uuid
  AND root.artifact_id=(e->'params'->>'artifact_id')::uuid
  AND root.sha256=e->'params'->>'sha256'
  AND root.state='ready' AND root.render_registered_at IS NOT NULL
  AND root.render_learning_checked AND root.render_learning IS NOT NULL
  AND item.ordinal=p_frame_ordinal AND item.asset_version_id=p_frame_version
  AND item.artifact_id=p_image AND item.sha256=p_image_sha
  AND version.master_artifact_id=p_image AND version.content_hash=p_image_sha
  AND version.review_state='accepted' AND asset.project_id=root.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
  AND image.output_role='working_image' AND image.media_type='application/json'
  AND image.state='ready' AND image.verified_at IS NOT NULL
  AND image.object_version NOT IN('','null')
  AND (image.expires_at IS NULL OR image.expires_at>clock_timestamp())
  AND EXISTS(SELECT 1 FROM photo.scene_groups g
   JOIN photo.scene_members member ON member.tenant_id=g.tenant_id
    AND member.project_id=g.project_id AND member.group_id=g.id
   WHERE g.tenant_id=sa.tenant_id AND g.project_id=sa.project_id
    AND g.analysis_id=sa.id AND member.asset_version_id=p_frame_version
    AND member.snapshot_ordinal=p_frame_ordinal)
  AND photo.v31_delivery_render_descriptor_live(p_expansion,p_token,p_ordinal,
   p_preset,p_task,(root.render_learning->'descriptor'->>'artifact_id')::uuid,
   root.render_learning->'descriptor'->>'sha256'));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_frame_source_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_frame_source_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_frame_source_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

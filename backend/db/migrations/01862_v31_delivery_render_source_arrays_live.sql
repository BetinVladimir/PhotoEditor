-- +goose Up
-- Typed RGB/alpha children of the current selected source WorkingImage.
-- The walker independently checks ref and shape against WorkingImage bytes.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_source_array_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_source uuid,p_source_sha text,
 p_array uuid,p_array_sha text,p_contract text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;
BEGIN
 IF p_contract NOT IN('WorkingPixels','WorkingAlpha') OR
  p_array_sha!~'^[0-9a-f]{64}$' OR NOT photo.v31_delivery_render_source_live(
  p_expansion,p_token,p_ordinal,p_preset,p_task,p_source,p_source_sha)
  THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts image
 JOIN photo.artifacts child ON child.tenant_id=image.tenant_id
  AND child.created_by_task_id=image.created_by_task_id
  AND child.produced_attempt=image.produced_attempt
  AND child.produced_fence=image.produced_fence
 WHERE image.tenant_id=(source->>'tenant_id')::uuid
  AND image.id=p_source AND image.sha256=p_source_sha
  AND image.created_by_task_id IS NOT NULL
  AND image.produced_attempt IS NOT NULL AND image.produced_fence IS NOT NULL
  AND child.id=p_array AND child.sha256=p_array_sha AND child.bytes>0
  AND child.media_type='application/x-npy' AND child.state='ready'
  AND child.verified_at IS NOT NULL AND child.object_version NOT IN('','null')
  AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
  AND ((p_contract='WorkingPixels' AND child.output_role IN('working_rgb','pixels'))
   OR (p_contract='WorkingAlpha' AND child.output_role='alpha')));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_source_array_live(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_source_array_live(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_source_array_live(uuid,uuid,integer,uuid,uuid,uuid,text,uuid,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

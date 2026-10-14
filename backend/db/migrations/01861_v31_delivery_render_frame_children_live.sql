-- +goose Up
-- Live child authority for one delegated render snapshot frame. The typed
-- walker validates each child ref against immutable descriptor/image JSON.
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_frame_faces_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_frame_ordinal integer,p_frame_version uuid,
 p_image uuid,p_image_sha text,p_faces uuid,p_faces_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;e jsonb;
BEGIN
 IF p_faces_sha!~'^[0-9a-f]{64}$' OR NOT photo.v31_delivery_render_frame_source_live(
  p_expansion,p_token,p_ordinal,p_preset,p_task,p_frame_ordinal,p_frame_version,
  p_image,p_image_sha) THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 e:=photo.v31_delivery_ancestor_task_evidence(p_expansion,p_token,p_ordinal,p_preset,p_task);
 IF e IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.v2_draft_uploads root
 JOIN photo.artifacts descriptor ON descriptor.tenant_id=root.tenant_id
  AND descriptor.id=(root.render_learning->'descriptor'->>'artifact_id')::uuid
  AND descriptor.sha256=root.render_learning->'descriptor'->>'sha256'
 JOIN photo.artifacts faces ON faces.tenant_id=descriptor.tenant_id
  AND faces.created_by_task_id=descriptor.created_by_task_id
  AND faces.produced_attempt=descriptor.produced_attempt
  AND faces.produced_fence=descriptor.produced_fence
 JOIN photo.output_uploads upload ON upload.tenant_id=faces.tenant_id
  AND upload.artifact_id=faces.id AND upload.task_id=faces.created_by_task_id
  AND upload.attempt=faces.produced_attempt AND upload.fence=faces.produced_fence
 WHERE root.tenant_id=(source->>'tenant_id')::uuid
  AND root.project_id=(source->>'project_id')::uuid
  AND root.artifact_id=(e->'params'->>'artifact_id')::uuid
  AND root.sha256=e->'params'->>'sha256'
  AND root.render_learning_checked AND root.render_learning IS NOT NULL
  AND faces.id=p_faces AND faces.sha256=p_faces_sha
  AND faces.output_role='faces' AND faces.media_type='application/json'
  AND faces.state='ready' AND faces.verified_at IS NOT NULL
  AND faces.object_version NOT IN('','null')
  AND (faces.expires_at IS NULL OR faces.expires_at>clock_timestamp())
  AND upload.state='ready' AND upload.role='faces'
  AND upload.sha256=faces.sha256 AND upload.object_version=faces.object_version
  AND upload.bytes=faces.bytes);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range THEN RETURN false;
END $$;
-- +goose StatementEnd
ALTER FUNCTION photo.v31_delivery_render_frame_faces_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_frame_faces_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_frame_faces_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementBegin
CREATE FUNCTION photo.v31_delivery_render_frame_array_live(
 p_expansion uuid,p_token uuid,p_ordinal integer,p_preset uuid,
 p_task uuid,p_frame_ordinal integer,p_frame_version uuid,
 p_image uuid,p_image_sha text,p_array uuid,p_array_sha text,p_contract text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source jsonb;
BEGIN
 IF p_contract NOT IN('WorkingPixels','WorkingAlpha') OR
  p_array_sha!~'^[0-9a-f]{64}$' OR NOT photo.v31_delivery_render_frame_source_live(
  p_expansion,p_token,p_ordinal,p_preset,p_task,p_frame_ordinal,p_frame_version,
  p_image,p_image_sha) THEN RETURN false;END IF;
 source:=photo.v31_delivery_export_evidence(p_expansion,p_token,p_ordinal,p_preset);
 IF source IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts image
 JOIN photo.artifacts child ON child.tenant_id=image.tenant_id
  AND child.created_by_task_id=image.created_by_task_id
  AND child.produced_attempt=image.produced_attempt
  AND child.produced_fence=image.produced_fence
 WHERE image.tenant_id=(source->>'tenant_id')::uuid
  AND image.id=p_image AND image.sha256=p_image_sha
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
ALTER FUNCTION photo.v31_delivery_render_frame_array_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v31_delivery_render_frame_array_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal,photo_maintenance;
GRANT EXECUTE ON FUNCTION photo.v31_delivery_render_frame_array_live(uuid,uuid,integer,uuid,uuid,integer,uuid,uuid,text,uuid,text,text)
 TO photo_scheduler;
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

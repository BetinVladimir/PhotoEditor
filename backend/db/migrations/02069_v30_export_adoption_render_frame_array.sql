-- +goose Up
-- Binary frame children require the same live descriptor/frame qualification
-- plus the physical source producer and exact claimed array object.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_render_frame_array(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_root uuid,p_root_sha text,p_descriptor uuid,p_descriptor_sha text,
 p_selected_version uuid,p_learning jsonb,p_lock text,p_ordinal integer,
 p_frame_version uuid,p_source uuid,p_source_sha text,
 p_contract text,p_array uuid,p_array_sha text,
 p_bucket text,p_object_key text,p_object_version text,p_bytes bigint)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE source photo.artifacts%ROWTYPE;
BEGIN
 IF p_contract IS NULL OR p_contract NOT IN ('WorkingPixels','WorkingAlpha')
  OR p_array IS NULL OR p_array=p_source
  OR coalesce(p_array_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' OR coalesce(p_object_key,'')=''
  OR coalesce(p_object_version,'') IN ('','null') OR p_bytes<1
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_array,p_array_sha,p_contract);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO source FROM photo.artifacts WHERE tenant_id=p_tenant
  AND id=p_source AND sha256=p_source_sha FOR SHARE;
 IF source.id IS NULL OR photo.v30_export_adoption_render_frame(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_root,p_root_sha,p_descriptor,p_descriptor_sha,p_selected_version,p_learning,
  p_lock,p_ordinal,p_frame_version,p_source,p_source_sha,'WorkingImage',
  p_source,p_source_sha,source.bucket,source.object_key,source.object_version,
  source.bytes) IS DISTINCT FROM true THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.artifacts child
  WHERE child.tenant_id=p_tenant AND child.id=p_array AND child.sha256=p_array_sha
   AND child.bucket=p_bucket AND child.object_key=p_object_key
   AND child.object_version=p_object_version AND child.bytes=p_bytes
   AND child.created_by_task_id=source.created_by_task_id
   AND source.created_by_task_id IS NOT NULL
   AND child.produced_attempt=source.produced_attempt
   AND source.produced_attempt IS NOT NULL
   AND child.produced_fence=source.produced_fence
   AND source.produced_fence IS NOT NULL
   AND child.state='ready' AND child.verified_at IS NOT NULL
   AND child.media_type='application/x-npy'
   AND (child.expires_at IS NULL OR child.expires_at>clock_timestamp())
   AND ((p_contract='WorkingPixels' AND child.output_role IN('working_rgb','pixels'))
    OR (p_contract='WorkingAlpha' AND child.output_role='alpha')));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_render_frame_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_render_frame_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_render_frame_array(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,jsonb,text,integer,uuid,uuid,text,text,uuid,text,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

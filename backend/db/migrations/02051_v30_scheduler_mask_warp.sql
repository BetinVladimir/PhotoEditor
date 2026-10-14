-- +goose Up
-- A MaskRef transform may reuse a field from another canonical task. Both
-- producers must bind to the same single source in the frozen export snapshot.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_mask_warp(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_parent uuid,p_parent_sha text,p_mask uuid,p_mask_sha text,
 p_field uuid,p_field_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_parent IS NULL OR p_mask IS NULL OR p_field IS NULL
  OR p_parent=p_mask OR p_parent=p_field OR p_mask=p_field
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_field_sha,'') !~ '^[0-9a-f]{64}$'
  OR photo.v30_export_adoption_mask_array(p_tenant,p_workflow,p_run,p_task,p_result,
   p_actor,p_key,p_request_sha,p_token,p_parent,p_parent_sha,p_mask,p_mask_sha)
   IS DISTINCT FROM true THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_field,p_field_sha,'DenseWarpField');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=item.artifact_id
  AND version.content_hash=item.sha256 AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts root ON root.tenant_id=item.tenant_id AND root.id=p_parent
  AND root.sha256=p_parent_sha
 JOIN photo.tasks mask_producer ON mask_producer.tenant_id=root.tenant_id
  AND mask_producer.id=root.created_by_task_id
 JOIN photo.artifacts field ON field.tenant_id=item.tenant_id AND field.id=p_field
  AND field.sha256=p_field_sha AND field.output_role='warp_field'
 JOIN photo.tasks field_producer ON field_producer.tenant_id=field.tenant_id
  AND field_producer.id=field.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=field_producer.tenant_id
  AND result.id=field_producer.canonical_result_id AND result.task_id=field_producer.id
  AND result.attempt=field_producer.attempt AND result.fencing_token=field_producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=field_producer.tenant_id
  AND attempt.task_id=field_producer.id AND attempt.attempt=field_producer.attempt
  AND attempt.fencing_token=field_producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=field_producer.tenant_id
  AND origin.id=field_producer.run_id
 LEFT JOIN photo.v2_workflows origin_workflow ON origin_workflow.tenant_id=origin.tenant_id
  AND origin_workflow.run_id=origin.id
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND mask_producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',item.artifact_id,'sha256',item.sha256))
  AND field_producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',item.artifact_id,'sha256',item.sha256))
  AND (origin_workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND NOT origin.cancel_requested AND field_producer.state='succeeded'
  AND NOT field_producer.cancel_requested AND field_producer.operation_version='1'
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_field,'sha256',p_field_sha))
  AND field.produced_attempt=field_producer.attempt
  AND field.produced_fence=field_producer.fencing_token
  AND attempt.backend=field_producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND field.media_type IN('application/octet-stream','application/x-npy')
  AND field.state='ready' AND field.verified_at IS NOT NULL
  AND field.object_version IS NOT NULL AND field.object_version NOT IN('','null')
  AND (field.expires_at IS NULL OR field.expires_at>clock_timestamp())
  AND (SELECT count(*) FROM jsonb_array_elements(field_producer.inputs) input(value)
   JOIN photo.artifacts source ON source.tenant_id=p_tenant
    AND source.id=(input.value->>'artifact_id')::uuid
    AND source.sha256=input.value->>'sha256'
    AND source.output_role='working_image')=1);
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_mask_warp(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_mask_warp(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_mask_warp(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

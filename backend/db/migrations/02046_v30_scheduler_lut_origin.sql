-- +goose Up
-- Current style.resolve provenance for a portable AbsoluteLUTArray, rooted in
-- the export's sealed inputs and frozen source snapshot.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_lut_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_image uuid,p_image_sha text,p_parent uuid,p_parent_sha text,p_array uuid,p_array_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_image IS NULL OR p_parent IS NULL OR p_array IS NULL
  OR p_image=p_parent OR p_image=p_array OR p_parent=p_array
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_parent_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_array_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_image,p_image_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_parent,p_parent_sha,'ResolvedStyle');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_array,p_array_sha,'AbsoluteLUTArray');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_image
  AND version.content_hash=p_image_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts source ON source.tenant_id=version.tenant_id AND source.id=p_image
  AND source.sha256=p_image_sha AND source.output_role='working_image'
 JOIN photo.artifacts resolved ON resolved.tenant_id=version.tenant_id
  AND resolved.id=p_parent AND resolved.sha256=p_parent_sha
 JOIN photo.tasks producer ON producer.tenant_id=resolved.tenant_id
  AND producer.id=resolved.created_by_task_id
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows producer_workflow ON producer_workflow.tenant_id=origin.tenant_id
  AND producer_workflow.run_id=origin.id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.artifacts lut ON lut.tenant_id=producer.tenant_id AND lut.id=p_array
  AND lut.sha256=p_array_sha AND lut.created_by_task_id=producer.id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id AND params.id=producer.params_artifact_id
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_image AND item.sha256=p_image_sha
  AND (producer_workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND NOT origin.cancel_requested
  AND producer.operation='style.resolve' AND producer.operation_version='1'
  AND producer.state='succeeded'
  AND attempt.finished_at IS NOT NULL AND attempt.backend IN('cpu','cuda','mps')
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND resolved.output_role='resolved_style' AND lut.output_role='absolute_lut'
  AND resolved.produced_attempt=producer.attempt
  AND resolved.produced_fence=producer.fencing_token
  AND lut.produced_attempt=producer.attempt AND lut.produced_fence=producer.fencing_token
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_parent,'sha256',p_parent_sha),
   jsonb_build_object('artifact_id',p_array,'sha256',p_array_sha))
  AND resolved.media_type='application/json' AND params.media_type='application/json'
  AND lut.media_type IN('application/octet-stream','application/x-npy')
  AND NOT EXISTS(SELECT 1 FROM (VALUES(source.state,source.verified_at,source.object_version,source.expires_at),
   (resolved.state,resolved.verified_at,resolved.object_version,resolved.expires_at),
   (lut.state,lut.verified_at,lut.object_version,lut.expires_at),
   (params.state,params.verified_at,params.object_version,params.expires_at))
   checked(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL
    OR version IN('','null') OR expires<=clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_lut_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_lut_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_lut_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

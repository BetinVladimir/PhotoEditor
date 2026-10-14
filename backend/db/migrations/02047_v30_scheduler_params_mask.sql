-- +goose Up
-- Current producer/input scope for an inline Params mask under a leased
-- canonical export. JSON occurrence and source geometry are checked in Go.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_params_mask(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_params uuid,p_params_sha text,p_operation text,
 p_source uuid,p_source_sha text,p_mask uuid,p_mask_sha text)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_params IS NULL OR p_source IS NULL OR p_mask IS NULL
  OR p_params=p_source OR p_params=p_mask OR p_source=p_mask
  OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_source_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_operation IS NULL OR length(p_operation) NOT BETWEEN 1 AND 120 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_params,p_params_sha,'Params');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_source,p_source_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_mask,p_mask_sha,'MaskPixels');
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_source
  AND version.content_hash=p_source_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id AND asset.id=version.asset_id
  AND asset.project_id=w.project_id AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.tasks producer ON producer.tenant_id=item.tenant_id
  AND producer.params_artifact_id=p_params
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=p_params AND params.sha256=p_params_sha
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows producer_workflow ON producer_workflow.tenant_id=origin.tenant_id
  AND producer_workflow.run_id=origin.id
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_source AND item.sha256=p_source_sha
  AND producer.operation=p_operation AND producer.operation_version='1'
  AND producer.state='succeeded' AND attempt.finished_at IS NOT NULL
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND NOT origin.cancel_requested
  AND (producer_workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_source,'sha256',p_source_sha),
   jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha))
  AND params.media_type='application/json' AND params.state='ready'
  AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL
  AND params.object_version NOT IN('','null')
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp()));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_params_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_params_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_params_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

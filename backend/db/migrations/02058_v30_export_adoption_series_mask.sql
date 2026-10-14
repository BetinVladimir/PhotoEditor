-- +goose Up
-- Exact measure-region mask bytes from a completed current mask producer.
-- Higher-level root/report/model/consent proof remains a separate authority.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_series_mask(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_measure uuid,p_attempt integer,p_fence bigint,p_stat uuid,p_stat_sha text,
 p_params uuid,p_params_sha text,p_bucket text,p_params_key text,
 p_params_version text,p_params_bytes bigint,p_inputs jsonb,
 p_image uuid,p_image_sha text,p_mask uuid,p_mask_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_mask IS NULL OR p_mask=p_image OR p_mask=p_params OR p_mask=p_stat
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$' THEN RETURN;END IF;
 IF NOT photo.v30_export_adoption_series_measure_inputs(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_measure,p_attempt,p_fence,p_stat,p_stat_sha,p_params,p_params_sha,p_bucket,
  p_params_key,p_params_version,p_params_bytes,p_inputs) THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_image,p_image_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT mask.bucket,mask.object_key,mask.object_version,
  mask.sha256,mask.bytes,mask.media_type
 FROM photo.artifacts mask
 JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id
  AND producer.id=mask.created_by_task_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id
  AND workflow.run_id=origin.id
 JOIN photo.snapshot_items item ON item.tenant_id=mask.tenant_id
  AND item.snapshot_id=w.input_snapshot_id AND item.artifact_id=p_image
  AND item.sha256=p_image_sha
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_image
  AND version.content_hash=p_image_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 WHERE mask.tenant_id=p_tenant AND mask.id=p_mask AND mask.sha256=p_mask_sha
  AND mask.bucket=p_bucket AND mask.bytes>0
  AND mask.media_type IN('application/octet-stream','application/x-npy')
  AND mask.state='ready' AND mask.verified_at IS NOT NULL
  AND mask.object_version IS NOT NULL AND mask.object_version NOT IN('','null')
  AND (mask.expires_at IS NULL OR mask.expires_at>clock_timestamp())
  AND producer.operation_version='1' AND producer.state='succeeded'
  AND NOT producer.cancel_requested AND NOT origin.cancel_requested
  AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND ((producer.operation='mask.person' AND mask.output_role='person_mask')
   OR (producer.operation='mask.face_skin' AND mask.output_role='skin_mask')
   OR (producer.operation='mask.prompt' AND mask.output_role='prompt_mask')
   OR (producer.operation='mask.transform' AND mask.output_role='transformed_mask'))
  AND mask.produced_attempt=producer.attempt AND mask.produced_fence=producer.fencing_token
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha))
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND (workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
 FOR SHARE OF mask,producer;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb,uuid,text,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb,uuid,text,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series_mask(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb,uuid,text,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

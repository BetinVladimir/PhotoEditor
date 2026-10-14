-- +goose Up
-- Discover the exact mask.person origin consumed by claimed Stats20. The
-- existing current-producer predicate rechecks root/report/fence and scope.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_stats_person_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_stats uuid,p_stats_sha text,p_stats_params uuid,p_stats_params_sha text,
 p_mask uuid,p_mask_sha text,p_image uuid,p_image_sha text,p_bucket text)
RETURNS TABLE(root_id uuid,root_sha text,original_id uuid,original_sha text,
 report_id uuid,report_sha text,mask_bucket text,mask_key text,
 mask_version text,mask_sha256 text,mask_bytes bigint,mask_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_stats IS NULL OR p_stats_params IS NULL OR p_mask IS NULL OR p_image IS NULL
  OR coalesce(p_stats_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_stats_params_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_mask_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_stats,p_stats_sha,'Stats20');
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_stats_params,p_stats_params_sha,'Params');
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_image,p_image_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT root.id,root.sha256,original.id,original.sha256,
  report.id,report.sha256,mask.bucket,mask.object_key,
  mask.object_version,mask.sha256,mask.bytes,mask.media_type
 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_image
  AND version.content_hash=p_image_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts stats ON stats.tenant_id=item.tenant_id
  AND stats.id=p_stats AND stats.sha256=p_stats_sha
 JOIN photo.tasks stat_task ON stat_task.tenant_id=stats.tenant_id
  AND stat_task.id=stats.created_by_task_id
 JOIN photo.artifacts stat_params ON stat_params.tenant_id=stat_task.tenant_id
  AND stat_params.id=stat_task.params_artifact_id
 JOIN photo.task_results stat_result ON stat_result.tenant_id=stat_task.tenant_id
  AND stat_result.id=stat_task.canonical_result_id AND stat_result.task_id=stat_task.id
  AND stat_result.attempt=stat_task.attempt AND stat_result.fencing_token=stat_task.fencing_token
 JOIN photo.task_attempts stat_attempt ON stat_attempt.tenant_id=stat_task.tenant_id
  AND stat_attempt.task_id=stat_task.id AND stat_attempt.attempt=stat_task.attempt
  AND stat_attempt.fencing_token=stat_task.fencing_token AND stat_attempt.finished_at IS NOT NULL
 JOIN photo.runs stat_run ON stat_run.tenant_id=stat_task.tenant_id
  AND stat_run.id=stat_task.run_id
 LEFT JOIN photo.v2_workflows stat_workflow ON stat_workflow.tenant_id=stat_run.tenant_id
  AND stat_workflow.run_id=stat_run.id
 JOIN photo.artifacts mask ON mask.tenant_id=stats.tenant_id
  AND mask.id=p_mask AND mask.sha256=p_mask_sha
 JOIN photo.tasks producer ON producer.tenant_id=mask.tenant_id
  AND producer.id=mask.created_by_task_id
 JOIN photo.artifacts root ON root.tenant_id=producer.tenant_id
  AND root.created_by_task_id=producer.id AND root.output_role='person_mask_ref'
 JOIN photo.artifacts report ON report.tenant_id=producer.tenant_id
  AND report.created_by_task_id=producer.id AND report.output_role='person_report'
 JOIN photo.artifacts original ON original.tenant_id=producer.tenant_id
  AND original.id=producer.params_artifact_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_image AND item.sha256=p_image_sha
  AND stats.output_role='stats' AND stats.produced_attempt=stat_task.attempt
  AND stats.produced_fence=stat_task.fencing_token
  AND stats.bucket=p_bucket AND stats.media_type='application/json'
  AND stat_task.operation='color.stats' AND stat_task.operation_version='1'
  AND stat_task.state='succeeded' AND NOT stat_task.cancel_requested
  AND stat_task.resource_class='cpu' AND stat_attempt.backend='cpu'
  AND stat_attempt.backend=stat_task.execution_profile->>'backend'
  AND stat_attempt.backend=stat_attempt.profile->>'backend'
  AND NOT stat_run.cancel_requested
  AND (stat_workflow.project_id=w.project_id
   OR stat_run.config_snapshot->>'project_id'=w.project_id::text)
  AND stat_params.id=p_stats_params AND stat_params.sha256=p_stats_params_sha
  AND stat_params.bucket=p_bucket AND stat_params.media_type='application/json'
  AND stat_params.bytes BETWEEN 2 AND 1048576
  AND stat_task.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha),
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND stat_result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stats,'sha256',p_stats_sha))
  AND mask.bucket=p_bucket AND mask.output_role='person_mask'
  AND mask.media_type IN('application/octet-stream','application/x-npy')
  AND mask.bytes>0 AND mask.produced_attempt=producer.attempt
  AND mask.produced_fence=producer.fencing_token
  AND root.produced_attempt=producer.attempt AND root.produced_fence=producer.fencing_token
  AND report.produced_attempt=producer.attempt AND report.produced_fence=producer.fencing_token
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_mask,'sha256',p_mask_sha),
   jsonb_build_object('artifact_id',root.id,'sha256',root.sha256),
   jsonb_build_object('artifact_id',report.id,'sha256',report.sha256))
  AND photo.v30_export_adoption_person_origin_current(
   p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
   version.id,p_image,p_image_sha,root.id,root.sha256,p_mask,p_mask_sha,
   producer.id,producer.attempt,producer.fencing_token,report.id,report.sha256)
  AND NOT EXISTS(SELECT 1 FROM (VALUES
   (stats.state,stats.verified_at,stats.object_version,stats.expires_at),
   (stat_params.state,stat_params.verified_at,stat_params.object_version,stat_params.expires_at),
   (mask.state,mask.verified_at,mask.object_version,mask.expires_at),
   (root.state,root.verified_at,root.object_version,root.expires_at),
   (report.state,report.verified_at,report.object_version,report.expires_at),
   (original.state,original.verified_at,original.object_version,original.expires_at))
   checked(state,verified,version,expires)
   WHERE state IS DISTINCT FROM 'ready' OR verified IS NULL OR version IS NULL
    OR version IN('','null') OR expires<=clock_timestamp())
  AND root.media_type='application/json' AND root.bytes BETWEEN 2 AND 1048576
  AND root.bucket=p_bucket
  AND original.media_type='application/json' AND original.bytes BETWEEN 2 AND 65536
  AND original.bucket=p_bucket
  AND report.bucket=p_bucket AND report.media_type='application/json'
  AND report.bytes BETWEEN 2 AND 8388608;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_stats_person_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_stats_person_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_stats_person_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,uuid,text,uuid,text,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

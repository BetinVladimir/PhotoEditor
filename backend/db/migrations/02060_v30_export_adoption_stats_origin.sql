-- +goose Up
-- Claimed Stats20 may expose only Params of its completed color.stats
-- producer, grounded in an accepted WorkingImage of the frozen snapshot.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_stats_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_stats uuid,p_stats_sha text,p_image uuid,p_image_sha text,
 p_bucket text,p_stats_key text,p_stats_version text,p_stats_bytes bigint)
RETURNS TABLE(params_id uuid,params_sha text,params_bucket text,params_key text,
 params_version text,params_bytes bigint,params_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_stats IS NULL OR p_image IS NULL OR p_stats=p_image
  OR coalesce(p_stats_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_image_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' OR coalesce(p_stats_key,'')=''
  OR coalesce(p_stats_version,'') IN ('','null') OR p_stats_bytes<2
 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_stats,p_stats_sha,'Stats20');
 IF NOT FOUND THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_result_input(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_image,p_image_sha,'WorkingImage');
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT params.id,params.sha256,params.bucket,params.object_key,
  params.object_version,params.bytes,params.media_type
 FROM photo.snapshot_items item
 JOIN photo.asset_versions version ON version.tenant_id=item.tenant_id
  AND version.id=item.asset_version_id AND version.master_artifact_id=p_image
  AND version.content_hash=p_image_sha AND version.review_state='accepted'
 JOIN photo.assets asset ON asset.tenant_id=version.tenant_id
  AND asset.id=version.asset_id AND asset.project_id=w.project_id
  AND asset.deleted_at IS NULL AND asset.upload_state='verified'
 JOIN photo.artifacts stats ON stats.tenant_id=item.tenant_id
  AND stats.id=p_stats AND stats.sha256=p_stats_sha
 JOIN photo.tasks producer ON producer.tenant_id=stats.tenant_id
  AND producer.id=stats.created_by_task_id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=producer.params_artifact_id
 JOIN photo.runs origin ON origin.tenant_id=producer.tenant_id
  AND origin.id=producer.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id
  AND workflow.run_id=origin.id
 JOIN photo.task_attempts attempt ON attempt.tenant_id=producer.tenant_id
  AND attempt.task_id=producer.id AND attempt.attempt=producer.attempt
  AND attempt.fencing_token=producer.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 WHERE item.tenant_id=p_tenant AND item.snapshot_id=w.input_snapshot_id
  AND item.artifact_id=p_image AND item.sha256=p_image_sha
  AND (workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND NOT origin.cancel_requested AND NOT producer.cancel_requested
  AND producer.operation='color.stats' AND producer.operation_version='1'
  AND producer.state='succeeded' AND producer.resource_class='cpu'
  AND attempt.backend='cpu' AND attempt.backend=producer.execution_profile->>'backend'
  AND attempt.backend=attempt.profile->>'backend'
  AND stats.output_role='stats' AND stats.state='ready'
  AND stats.verified_at IS NOT NULL AND stats.produced_attempt=producer.attempt
  AND stats.produced_fence=producer.fencing_token
  AND stats.bucket=p_bucket AND stats.object_key=p_stats_key
  AND stats.object_version=p_stats_version AND stats.bytes=p_stats_bytes
  AND stats.media_type='application/json'
  AND (stats.expires_at IS NULL OR stats.expires_at>clock_timestamp())
  AND producer.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_image,'sha256',p_image_sha))
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stats,'sha256',p_stats_sha))
  AND params.bucket=p_bucket AND params.state='ready'
  AND params.verified_at IS NOT NULL AND params.media_type='application/json'
  AND params.bytes BETWEEN 2 AND 1048576
  AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
 FOR SHARE OF item,stats,producer,params;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_stats_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_stats_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_stats_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,uuid,text,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

-- +goose Up
-- Return the exact Params of the canonical series.measure producer for one
-- stat already bound to the claimed series.balance input.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_series_measure_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_balance uuid,p_attempt integer,p_fence bigint,p_balance_params uuid,p_balance_params_sha text,
 p_balance_bucket text,p_balance_key text,p_balance_version text,p_balance_bytes bigint,
 p_stat uuid,p_stat_sha text,p_stat_key text,p_stat_version text,p_stat_bytes bigint)
RETURNS TABLE(producer_task uuid,producer_attempt integer,producer_fence bigint,
 params_id uuid,params_sha text,params_bucket text,params_key text,
 params_version text,params_bytes bigint,params_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF coalesce(p_stat_key,'')='' OR coalesce(p_stat_version,'') IN ('','null')
  OR p_stat_bytes<2 OR p_stat_bytes>1048576 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_series_stat(
  p_tenant,p_workflow,p_run,p_task,p_result,p_actor,p_key,p_request_sha,p_token,
  p_balance,p_attempt,p_fence,p_balance_params,p_balance_params_sha,
  p_balance_bucket,p_balance_key,p_balance_version,p_balance_bytes,p_stat,p_stat_sha);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT measure.id,measure.attempt,measure.fencing_token,
  params.id,params.sha256,params.bucket,params.object_key,
  params.object_version,params.bytes,params.media_type
 FROM photo.artifacts stats
 JOIN photo.tasks measure ON measure.tenant_id=stats.tenant_id
  AND measure.id=stats.created_by_task_id
 JOIN photo.artifacts params ON params.tenant_id=measure.tenant_id
  AND params.id=measure.params_artifact_id
 JOIN photo.task_results result ON result.tenant_id=measure.tenant_id
  AND result.id=measure.canonical_result_id AND result.task_id=measure.id
  AND result.attempt=measure.attempt AND result.fencing_token=measure.fencing_token
 JOIN photo.task_attempts attempt ON attempt.tenant_id=measure.tenant_id
  AND attempt.task_id=measure.id AND attempt.attempt=measure.attempt
  AND attempt.fencing_token=measure.fencing_token AND attempt.finished_at IS NOT NULL
 JOIN photo.runs origin ON origin.tenant_id=measure.tenant_id AND origin.id=measure.run_id
 LEFT JOIN photo.v2_workflows workflow ON workflow.tenant_id=origin.tenant_id
  AND workflow.run_id=origin.id
 WHERE stats.tenant_id=p_tenant AND stats.id=p_stat AND stats.sha256=p_stat_sha
  AND stats.bucket=p_balance_bucket AND stats.object_key=p_stat_key
  AND stats.object_version=p_stat_version AND stats.bytes=p_stat_bytes
  AND stats.output_role='series_stats' AND stats.produced_attempt=measure.attempt
  AND stats.produced_fence=measure.fencing_token
  AND stats.state='ready' AND stats.verified_at IS NOT NULL
  AND (stats.expires_at IS NULL OR stats.expires_at>clock_timestamp())
  AND measure.operation='series.measure' AND measure.operation_version='1'
  AND measure.state='succeeded' AND NOT measure.cancel_requested
  AND measure.resource_class='cpu' AND attempt.backend='cpu'
  AND attempt.profile->>'backend'='cpu' AND measure.execution_profile->>'backend'='cpu'
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stat,'sha256',p_stat_sha))
  AND NOT origin.cancel_requested
  AND (workflow.project_id=w.project_id
   OR origin.config_snapshot->>'project_id'=w.project_id::text)
  AND params.bucket=p_balance_bucket AND params.media_type='application/json'
  AND params.bytes BETWEEN 2 AND 1048576 AND params.state='ready'
  AND params.verified_at IS NOT NULL AND params.object_version IS NOT NULL
  AND params.object_version NOT IN('','null')
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp());
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series_measure_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series_measure_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series_measure_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text,text,text,bigint)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

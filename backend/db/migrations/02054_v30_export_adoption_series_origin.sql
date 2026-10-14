-- +goose Up
-- The balance Params and its exact stats inputs are exposed only through an
-- already claimed SeriesCorrections producer. No caller session is consulted.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_series_origin(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_series uuid,p_series_sha text,p_bucket text,p_object_key text,
 p_version text,p_bytes bigint)
RETURNS TABLE(producer_task uuid,producer_attempt integer,producer_fence bigint,
 params_id uuid,params_sha text,params_bucket text,params_key text,
 params_version text,params_bytes bigint,params_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
BEGIN
 IF NOT photo.v30_export_adoption_series(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token,p_series,p_series_sha,p_bucket,
  p_object_key,p_version,p_bytes) THEN RETURN;END IF;
 RETURN QUERY SELECT producer.id,producer.attempt,producer.fencing_token,
  params.id,params.sha256,params.bucket,params.object_key,
  params.object_version,params.bytes,params.media_type
 FROM photo.artifacts series
 JOIN photo.tasks producer ON producer.tenant_id=series.tenant_id
  AND producer.id=series.created_by_task_id
 JOIN photo.artifacts params ON params.tenant_id=producer.tenant_id
  AND params.id=producer.params_artifact_id
 JOIN photo.task_results result ON result.tenant_id=producer.tenant_id
  AND result.id=producer.canonical_result_id AND result.task_id=producer.id
  AND result.attempt=producer.attempt AND result.fencing_token=producer.fencing_token
 WHERE series.tenant_id=p_tenant AND series.id=p_series AND series.sha256=p_series_sha
  AND series.bucket=p_bucket AND series.object_key=p_object_key
  AND series.object_version=p_version AND series.bytes=p_bytes
  AND series.output_role='series_corrections' AND series.media_type='application/json'
  AND series.produced_attempt=producer.attempt AND series.produced_fence=producer.fencing_token
  AND producer.operation='series.balance' AND producer.operation_version='1'
  AND producer.state='succeeded' AND NOT producer.cancel_requested
  AND result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_series,'sha256',p_series_sha))
  AND params.media_type='application/json' AND params.bytes BETWEEN 2 AND 1048576
  AND params.state='ready' AND params.verified_at IS NOT NULL
  AND params.object_version IS NOT NULL AND params.object_version NOT IN('','null')
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp());
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series_origin(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,text,text,text,text,bigint)
 TO photo_scheduler;

CREATE FUNCTION photo.v30_export_adoption_series_stat(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_balance uuid,p_attempt integer,p_fence bigint,p_params uuid,p_params_sha text,
 p_params_bucket text,p_params_key text,p_params_version text,p_params_bytes bigint,
 p_stat uuid,p_stat_sha text)
RETURNS TABLE(bucket text,object_key text,object_version text,sha256 text,bytes bigint,media_type text)
LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_balance IS NULL OR p_attempt<1 OR p_fence<1 OR p_params IS NULL
  OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_stat_sha,'') !~ '^[0-9a-f]{64}$'
  OR p_stat IS NULL OR p_stat=p_params OR coalesce(p_params_bucket,'')=''
  OR coalesce(p_params_key,'')='' OR coalesce(p_params_version,'') IN ('','null')
  OR p_params_bytes<2 OR p_params_bytes>1048576 THEN RETURN;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN;END IF;
 RETURN QUERY SELECT stats.bucket,stats.object_key,stats.object_version,
  stats.sha256,stats.bytes,stats.media_type
 FROM photo.tasks balance
 JOIN photo.artifacts params ON params.tenant_id=balance.tenant_id
  AND params.id=balance.params_artifact_id
 JOIN photo.artifacts series ON series.tenant_id=balance.tenant_id
  AND series.created_by_task_id=balance.id AND series.output_role='series_corrections'
 JOIN photo.v2_task_inputs sealed ON sealed.tenant_id=series.tenant_id
  AND sealed.task_id=p_task AND sealed.artifact_id=series.id
  AND sealed.sha256=series.sha256 AND sealed.contract='SeriesCorrections'
 JOIN photo.task_results balance_result ON balance_result.tenant_id=balance.tenant_id
  AND balance_result.id=balance.canonical_result_id AND balance_result.task_id=balance.id
  AND balance_result.attempt=balance.attempt AND balance_result.fencing_token=balance.fencing_token
 JOIN photo.runs balance_run ON balance_run.tenant_id=balance.tenant_id
  AND balance_run.id=balance.run_id
 LEFT JOIN photo.v2_workflows balance_workflow ON balance_workflow.tenant_id=balance_run.tenant_id
  AND balance_workflow.run_id=balance_run.id
 JOIN photo.artifacts stats ON stats.tenant_id=balance.tenant_id
  AND stats.id=p_stat AND stats.sha256=p_stat_sha
 JOIN photo.tasks measure ON measure.tenant_id=stats.tenant_id
  AND measure.id=stats.created_by_task_id
 JOIN photo.task_results measure_result ON measure_result.tenant_id=measure.tenant_id
  AND measure_result.id=measure.canonical_result_id AND measure_result.task_id=measure.id
  AND measure_result.attempt=measure.attempt AND measure_result.fencing_token=measure.fencing_token
 JOIN photo.task_attempts measure_attempt ON measure_attempt.tenant_id=measure.tenant_id
  AND measure_attempt.task_id=measure.id AND measure_attempt.attempt=measure.attempt
  AND measure_attempt.fencing_token=measure.fencing_token AND measure_attempt.finished_at IS NOT NULL
 JOIN photo.runs measure_run ON measure_run.tenant_id=measure.tenant_id
  AND measure_run.id=measure.run_id
 LEFT JOIN photo.v2_workflows measure_workflow ON measure_workflow.tenant_id=measure_run.tenant_id
  AND measure_workflow.run_id=measure_run.id
 WHERE balance.tenant_id=p_tenant AND balance.id=p_balance
  AND balance.attempt=p_attempt AND balance.fencing_token=p_fence
  AND balance.operation='series.balance' AND balance.operation_version='1'
  AND balance.state='succeeded' AND NOT balance.cancel_requested
  AND balance.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stat,'sha256',p_stat_sha))
  AND params.id=p_params AND params.sha256=p_params_sha
  AND params.bucket=p_params_bucket AND params.object_key=p_params_key
  AND params.object_version=p_params_version AND params.bytes=p_params_bytes
  AND params.media_type='application/json' AND params.state='ready'
  AND params.verified_at IS NOT NULL AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
  AND series.produced_attempt=balance.attempt AND series.produced_fence=balance.fencing_token
  AND photo.v30_export_adoption_series(p_tenant,p_workflow,p_run,p_task,p_result,
   p_actor,p_key,p_request_sha,p_token,series.id,series.sha256,
   series.bucket,series.object_key,series.object_version,series.bytes)
  AND series.state='ready' AND series.verified_at IS NOT NULL
  AND series.object_version IS NOT NULL AND series.object_version NOT IN('','null')
  AND (series.expires_at IS NULL OR series.expires_at>clock_timestamp())
  AND balance_result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',series.id,'sha256',series.sha256))
  AND stats.bucket=p_params_bucket AND stats.bytes BETWEEN 2 AND 1048576
  AND stats.media_type='application/json' AND stats.output_role='series_stats'
  AND stats.produced_attempt=measure.attempt AND stats.produced_fence=measure.fencing_token
  AND stats.state='ready' AND stats.verified_at IS NOT NULL
  AND stats.object_version IS NOT NULL AND stats.object_version NOT IN('','null')
  AND (stats.expires_at IS NULL OR stats.expires_at>clock_timestamp())
  AND measure.operation='series.measure' AND measure.operation_version='1'
  AND measure.state='succeeded' AND NOT measure.cancel_requested
  AND measure.resource_class='cpu' AND measure_attempt.backend='cpu'
  AND measure_attempt.profile->>'backend'='cpu'
  AND measure.execution_profile->>'backend'='cpu'
  AND measure_result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stat,'sha256',p_stat_sha))
  AND NOT balance_run.cancel_requested AND NOT measure_run.cancel_requested
  AND (balance_workflow.project_id=w.project_id
   OR balance_run.config_snapshot->>'project_id'=w.project_id::text)
  AND (measure_workflow.project_id=w.project_id
   OR measure_run.config_snapshot->>'project_id'=w.project_id::text)
 FOR SHARE OF balance,params,series,stats,measure;
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series_stat(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series_stat(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series_stat(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,text,text,text,bigint,uuid,text)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd

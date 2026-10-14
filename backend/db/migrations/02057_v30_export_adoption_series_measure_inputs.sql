-- +goose Up
-- Exact canonical series.measure input inventory, including pixels, alpha and
-- region masks. Physical mask provenance is checked by the native caller.
-- +goose StatementBegin
CREATE FUNCTION photo.v30_export_adoption_series_measure_inputs(
 p_tenant uuid,p_workflow uuid,p_run uuid,p_task uuid,p_result uuid,
 p_actor uuid,p_key text,p_request_sha text,p_token uuid,
 p_measure uuid,p_attempt integer,p_fence bigint,p_stat uuid,p_stat_sha text,
 p_params uuid,p_params_sha text,p_bucket text,p_params_key text,
 p_params_version text,p_params_bytes bigint,p_inputs jsonb)
RETURNS boolean LANGUAGE plpgsql SECURITY DEFINER SET search_path=pg_catalog,photo AS $$
DECLARE w photo.v2_workflows%ROWTYPE;
BEGIN
 IF p_measure IS NULL OR p_attempt<1 OR p_fence<1 OR p_params IS NULL
  OR p_stat IS NULL OR coalesce(p_params_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_stat_sha,'') !~ '^[0-9a-f]{64}$'
  OR coalesce(p_bucket,'')='' OR coalesce(p_params_key,'')=''
  OR coalesce(p_params_version,'') IN ('','null')
  OR p_params_bytes<2 OR p_params_bytes>1048576
  OR jsonb_typeof(p_inputs) IS DISTINCT FROM 'array'
  OR jsonb_array_length(p_inputs)<3 OR jsonb_array_length(p_inputs)>40
 THEN RETURN false;END IF;
 PERFORM 1 FROM photo.v30_export_adoption_root_output(p_tenant,p_workflow,p_run,p_task,p_result,
  p_actor,p_key,p_request_sha,p_token);
 IF NOT FOUND THEN RETURN false;END IF;
 SELECT * INTO w FROM photo.v2_workflows WHERE tenant_id=p_tenant AND id=p_workflow
  AND run_id=p_run FOR SHARE;
 IF w.id IS NULL THEN RETURN false;END IF;
 RETURN EXISTS(SELECT 1 FROM photo.tasks measure
 JOIN photo.artifacts params ON params.tenant_id=measure.tenant_id
  AND params.id=measure.params_artifact_id
 JOIN photo.artifacts stats ON stats.tenant_id=measure.tenant_id
  AND stats.created_by_task_id=measure.id AND stats.id=p_stat AND stats.sha256=p_stat_sha
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
 JOIN photo.tasks balance ON balance.tenant_id=measure.tenant_id
  AND balance.inputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stat,'sha256',p_stat_sha))
 JOIN photo.artifacts series ON series.tenant_id=balance.tenant_id
  AND series.created_by_task_id=balance.id AND series.output_role='series_corrections'
 JOIN photo.v2_task_inputs sealed ON sealed.tenant_id=series.tenant_id
  AND sealed.task_id=p_task AND sealed.artifact_id=series.id
  AND sealed.sha256=series.sha256 AND sealed.contract='SeriesCorrections'
 WHERE measure.tenant_id=p_tenant AND measure.id=p_measure
  AND measure.attempt=p_attempt AND measure.fencing_token=p_fence
  AND measure.operation='series.measure' AND measure.operation_version='1'
  AND measure.state='succeeded' AND NOT measure.cancel_requested
  AND measure.resource_class='cpu' AND measure_attempt.backend='cpu'
  AND measure_attempt.profile->>'backend'='cpu'
  AND measure.execution_profile->>'backend'='cpu'
  AND measure.inputs=p_inputs
  AND stats.output_role='series_stats' AND stats.produced_attempt=measure.attempt
  AND stats.produced_fence=measure.fencing_token
  AND stats.state='ready' AND stats.verified_at IS NOT NULL
  AND stats.object_version IS NOT NULL AND stats.object_version NOT IN('','null')
  AND (stats.expires_at IS NULL OR stats.expires_at>clock_timestamp())
  AND measure_result.outputs @> jsonb_build_array(
   jsonb_build_object('artifact_id',p_stat,'sha256',p_stat_sha))
  AND params.id=p_params AND params.sha256=p_params_sha
  AND params.bucket=p_bucket AND params.object_key=p_params_key
  AND params.object_version=p_params_version AND params.bytes=p_params_bytes
  AND params.media_type='application/json' AND params.state='ready'
  AND params.verified_at IS NOT NULL
  AND (params.expires_at IS NULL OR params.expires_at>clock_timestamp())
  AND NOT measure_run.cancel_requested
  AND (measure_workflow.project_id=w.project_id
   OR measure_run.config_snapshot->>'project_id'=w.project_id::text)
  AND photo.v30_export_adoption_series(p_tenant,p_workflow,p_run,p_task,p_result,
   p_actor,p_key,p_request_sha,p_token,series.id,series.sha256,
   series.bucket,series.object_key,series.object_version,series.bytes));
EXCEPTION WHEN invalid_text_representation OR numeric_value_out_of_range OR invalid_parameter_value THEN RETURN false;
END $$;
ALTER FUNCTION photo.v30_export_adoption_series_measure_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb)
 OWNER TO photo_fn_owner;
REVOKE ALL ON FUNCTION photo.v30_export_adoption_series_measure_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb)
 FROM PUBLIC,photo_api,photo_worker,photo_gateway,photo_portal;
GRANT EXECUTE ON FUNCTION photo.v30_export_adoption_series_measure_inputs(uuid,uuid,uuid,uuid,uuid,uuid,text,text,uuid,uuid,integer,bigint,uuid,text,uuid,text,text,text,text,bigint,jsonb)
 TO photo_scheduler;
-- +goose StatementEnd
-- +goose Down
-- +goose StatementBegin
DO $$ BEGIN RAISE EXCEPTION 'Forward-only migration';END $$;
-- +goose StatementEnd
